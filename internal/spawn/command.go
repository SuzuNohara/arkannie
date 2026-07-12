// Package spawn builds deterministic claude invocations (RunSpec) and runs
// them as isolated process groups with timeout kill. Containment follows
// plan A from spec/notes-containment.md: agnostic agents are confined by
// tool omission plus an explicit disallow belt; no path-scoped rules in v1.
package spawn

import (
	"fmt"
	"strings"
	"time"

	"arkannie/internal/config"
	"arkannie/internal/registry"
)

// RunSpec is the deterministic contract for one claude spawn.
type RunSpec struct {
	PromptFile, Model, Cwd                 string
	AllowedTools, DisallowedTools, AddDirs []string
	Timeout                                time.Duration
}

// Consent carries the trust-boundary grants passed from argv. Workspace lets
// executor-scoped agents write in the invoker's cwd (--allow-workspace);
// LayerAll/LayerList permit dispatching layer agents that run inside another
// AI's origin (--allow-layer, bare or with a name whitelist). The two
// surfaces are distinct: workspace = write in projects; layer = execute a
// foreign CLAUDE.md in its own directory.
type Consent struct {
	Workspace bool
	LayerAll  bool
	LayerList []string
}

// layerAllowed reports whether consent permits dispatching the named layer
// agent: --allow-layer bare (LayerAll) covers every layer agent, otherwise
// the bare name must appear in the whitelist.
func (c Consent) layerAllowed(name string) bool {
	if c.LayerAll {
		return true
	}
	for _, n := range c.LayerList {
		if n == name {
			return true
		}
	}
	return false
}

// bareName strips the [ ] dispatch brackets an agent command carries (e.g.
// "[echo]") so it matches the bare names passed to --allow-layer.
func bareName(command string) string {
	return strings.Trim(command, "[]")
}

// PreDispatchError reports a failure detected before any process is spawned.
// Class follows the protocol error classes: 'A' usage error, 'B' escalation.
type PreDispatchError struct {
	Class byte
	Msg   string
}

func (e *PreDispatchError) Error() string {
	return fmt.Sprintf("pre-dispatch class %c: %s", e.Class, e.Msg)
}

// grantOrder fixes the deterministic expansion order of grants into tools.
var grantOrder = []string{"read", "write", "execute", "network"}

// grantTools is the fixed grant → tools mapping.
var grantTools = map[string][]string{
	"read":    {"Read", "Grep", "Glob"},
	"write":   {"Write", "Edit"},
	"execute": {"Bash"},
	"network": {"WebFetch", "WebSearch"},
}

// agnosticDisallowed is the containment belt for agnostic agents. VAL-12
// already forbids write/execute grants; this denies the tools explicitly
// as a second lock (plan A, spec/notes-containment.md).
var agnosticDisallowed = []string{"Write", "Edit", "Bash", "NotebookEdit"}

// SideEffectTools are the write and command-execution tools — the ones a
// corrective retry of an executor agent must not hold. Mirrors the agnostic
// containment belt.
func SideEffectTools() []string {
	return append([]string(nil), agnosticDisallowed...)
}

// WithoutSideEffects returns allowed with every write/execute tool removed, so
// a demoted retry can read and inspect the workspace but cannot mutate it.
func WithoutSideEffects(allowed []string) []string {
	blocked := make(map[string]bool, len(agnosticDisallowed))
	for _, t := range agnosticDisallowed {
		blocked[t] = true
	}
	out := make([]string, 0, len(allowed))
	for _, t := range allowed {
		if !blocked[t] {
			out = append(out, t)
		}
	}
	return out
}

// PlusSideEffects merges the side-effect belt into disallowed (deduped), the
// denylist half of demoting a retry to read-only.
func PlusSideEffects(disallowed []string) []string {
	seen := make(map[string]bool, len(disallowed)+len(agnosticDisallowed))
	out := make([]string, 0, len(disallowed)+len(agnosticDisallowed))
	for _, t := range append(append([]string(nil), disallowed...), agnosticDisallowed...) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// BuildRunSpec derives the spawn contract for one operation dispatch.
// It never touches the filesystem and never spawns anything. Timeout is
// resolved first so a §4.2 usage error (negative flag) takes precedence over
// any consent rejection.
func BuildRunSpec(a *registry.Agent, op *registry.Operation, promptFile, runDir, invokerCwd string,
	consent Consent, timeoutFlagSecs int, cfg *config.Config) (RunSpec, error) {
	timeout, err := resolveTimeout(timeoutFlagSecs, a.Timeout, cfg.TimeoutDefault)
	if err != nil {
		return RunSpec{}, err
	}
	spec := RunSpec{
		PromptFile:   promptFile,
		Model:        a.Model,
		AllowedTools: expandGrants(op.Grants, true),
		Timeout:      timeout,
	}
	// Disallow belt depends only on scope: executor gets the grant complement,
	// agnostic gets the fixed containment belt. Layer does not change the belt.
	if a.Scope == "executor" {
		spec.DisallowedTools = expandGrants(op.Grants, false)
	} else {
		spec.DisallowedTools = append([]string(nil), agnosticDisallowed...)
	}
	// Layer spawn (decisions 5/6): cwd = the absorbed AI's origin, gated by a
	// dedicated layer consent that is strictly stronger than --allow-workspace
	// (so it is not additionally required). The belt is already set by scope,
	// so an agnostic layer agent stays confined even inside its own origin.
	if a.Layer != nil {
		name := bareName(a.Command)
		if !consent.layerAllowed(name) {
			return RunSpec{}, &PreDispatchError{
				Class: 'B',
				Msg:   fmt.Sprintf("agent [%s] runs as a layer over %s: dispatch requires --allow-layer", name, a.Layer.Origin),
			}
		}
		spec.Cwd = a.Layer.Origin
		return spec, nil
	}
	if a.Scope == "executor" {
		if !consent.Workspace {
			return RunSpec{}, &PreDispatchError{
				Class: 'B',
				Msg:   fmt.Sprintf("agent [%s] has scope executor: workspace access requires --allow-workspace", bareName(a.Command)),
			}
		}
		spec.Cwd = invokerCwd
		return spec, nil
	}
	spec.Cwd = runDir
	return spec, nil
}

// expandGrants maps grants to tools in the fixed read,write,execute,network
// order. granted=true returns the union of granted tools; granted=false
// returns the complement (every mapped tool whose grant was not given).
func expandGrants(grants []string, granted bool) []string {
	set := make(map[string]bool, len(grants))
	for _, g := range grants {
		set[g] = true
	}
	var tools []string
	for _, g := range grantOrder {
		if set[g] == granted {
			tools = append(tools, grantTools[g]...)
		}
	}
	return tools
}

// resolveTimeout applies the §4.1 hierarchy: --timeout flag > agent yaml
// timeout > config timeout_default. A negative flag is a §4.2 usage error.
func resolveTimeout(flagSecs, agentSecs, defaultSecs int) (time.Duration, error) {
	if flagSecs < 0 {
		return 0, &PreDispatchError{
			Class: 'A',
			Msg:   fmt.Sprintf("--timeout must be a positive integer, got %d", flagSecs),
		}
	}
	secs := defaultSecs
	switch {
	case flagSecs > 0:
		secs = flagSecs
	case agentSecs > 0:
		secs = agentSecs
	}
	return time.Duration(secs) * time.Second, nil
}
