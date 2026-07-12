// Package scheduler executes a parsed Ann v0.1 program: sequential
// dispatch with RAM scoping, parallel {} blocks, foreach/loop control
// flow, checkpoints (§10) and the trinary error classes A/B/C (§7.4).
// It orchestrates the pure and I/O packages (ann, ram, registry,
// dispatch, spawn, envelope, checkpoint, output) and never talks to the
// real claude CLI itself — the Spawner is injected.
package scheduler

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"arkannie/internal/ann"
	"arkannie/internal/envelope"
	"arkannie/internal/ram"
)

// Escalation is a protocol error surfaced to the developer per §7.4.
// Class is 'A' (notice, continue), 'B' (stop, propose) or 'C' (stop,
// authorization required). Command/Operation/ID populate the §8 Context
// block; Proposal carries the recovery text (B) or authorization text (C).
type Escalation struct {
	Class     byte
	Title     string
	Detail    string
	Proposal  string
	Command   string
	Operation string
	ID        string
}

// Format renders the exact §8 error format of spec/agent-protocol.md. The
// layout is non-negotiable: header, Context block, Detail, and a recovery
// or authorization section for Class B / C respectively.
func (e *Escalation) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[arkannie] ERROR — %s\n\n", e.Title)
	b.WriteString("Context:\n")
	fmt.Fprintf(&b, "  command: %s\n", e.Command)
	fmt.Fprintf(&b, "  operation: %s\n", e.Operation)
	fmt.Fprintf(&b, "  id: %s\n", e.ID)
	fmt.Fprintf(&b, "  class: %c\n", e.Class)
	b.WriteString("\nDetail:\n")
	b.WriteString(indentLines(e.Detail))
	switch e.Class {
	case 'B':
		b.WriteString("\nProposed recovery:\n")
		b.WriteString(indentLines(e.Proposal))
	case 'C':
		b.WriteString("\nAuthorization required:\n")
		b.WriteString(indentLines(e.Proposal))
	}
	return b.String()
}

// indentLines prefixes every line of s with two spaces and guarantees a
// trailing newline, matching the §8 Detail/recovery indentation.
func indentLines(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString("  ")
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return b.String()
}

// escalateC builds a Class C escalation (irreversible action detected).
// v1 has no automatic triggers; the method exists so an executor agent
// that detects a trigger can surface it with zero recovery proposal.
func (s *Scheduler) escalateC(title, detail, command, operation, id string) *Escalation {
	return &Escalation{
		Class:     'C',
		Title:     title,
		Detail:    detail,
		Command:   brackets(command),
		Operation: operation,
		ID:        id,
		Proposal:  "Explicit developer authorization is required before Arkannie proceeds.",
	}
}

// brackets wraps a bare command name in the canonical [name] token.
func brackets(cmd string) string {
	if cmd == "" || strings.HasPrefix(cmd, "[") {
		return cmd
	}
	return "[" + cmd + "]"
}

// escUnknownCommand is the safety-belt escalation for a command that
// ValidateCommands should already have rejected (§5.1).
func escUnknownCommand(d *ann.Dispatch, id string) *Escalation {
	return &Escalation{
		Class:    'B',
		Title:    "unknown command",
		Detail:   fmt.Sprintf("command %s is not registered in the agent registry.", brackets(d.Command)),
		Command:  brackets(d.Command),
		ID:       id,
		Proposal: "Register the agent under .agents/ or correct the command name.",
	}
}

// escPredispatch converts a pre-dispatch Class B/C failure (§5) into an
// escalation; Class A never reaches here (handled as a notice).
func escPredispatch(d *ann.Dispatch, operation, id, msg string, class byte) *Escalation {
	e := &Escalation{
		Class:     class,
		Title:     "pre-dispatch failure",
		Detail:    msg,
		Command:   brackets(d.Command),
		Operation: operation,
		ID:        id,
	}
	if class == 'B' {
		e.Proposal = "Fix the dispatch atom and re-run the program."
	}
	return e
}

// escInternal reports an unexpected assembly failure (prompt render, run
// dir creation) as Class B.
func escInternal(d *ann.Dispatch, operation, id string, err error) *Escalation {
	return &Escalation{
		Class:     'B',
		Title:     "dispatch assembly failed",
		Detail:    err.Error(),
		Command:   brackets(d.Command),
		Operation: operation,
		ID:        id,
		Proposal:  "Inspect the agent contract and run directory permissions.",
	}
}

// escSpawn reports a spawn I/O failure (the process could not be run) as
// Class B.
func escSpawn(prep *preparedDispatch, err error) *Escalation {
	return &Escalation{
		Class:     'B',
		Title:     "spawn failed",
		Detail:    err.Error(),
		Command:   brackets(prep.d.Command),
		Operation: prep.opName,
		ID:        prep.did,
		Proposal:  "Verify the claude binary and re-run the program.",
	}
}

// escMalformed is the §2 escalation raised after a second protocol
// violation: it quotes the raw return verbatim.
func escMalformed(prep *preparedDispatch, env *envelope.Envelope, v *envelope.Violation) *Escalation {
	raw := ""
	if env != nil {
		raw = env.Raw
	}
	detail := fmt.Sprintf("Wave %s (--id=%s) returned a malformed envelope.\nViolation: %s\nRaw return: %s",
		brackets(prep.d.Command), prep.did, v.Msg, raw)
	return &Escalation{
		Class:     'B',
		Title:     "malformed envelope",
		Detail:    detail,
		Command:   brackets(prep.d.Command),
		Operation: prep.opName,
		ID:        prep.did,
		Proposal:  "Inspect the raw return and re-dispatch, or fix the agent contract.",
	}
}

// escUnhandledError is the §2.2 default for an error status with no
// error -> {} handler.
func escUnhandledError(d *ann.Dispatch, env *envelope.Envelope) *Escalation {
	reason := ""
	if m, ok := env.Payload.(map[string]any); ok {
		reason, _ = m["reason"].(string)
	}
	detail := fmt.Sprintf("Wave %s returned status error with no error handler.\nreason: %s",
		brackets(d.Command), reason)
	return &Escalation{
		Class:    'B',
		Title:    "unhandled wave error",
		Detail:   detail,
		Command:  brackets(d.Command),
		ID:       env.ID,
		Proposal: "Add an error -> {} handler or fix the wave.",
	}
}

// escOrphan reports an envelope whose id matches no active dispatch in the
// parallel block (§3 correlation rule 4).
func escOrphan(id string) *Escalation {
	return &Escalation{
		Class:    'B',
		Title:    "orphan envelope",
		Detail:   fmt.Sprintf("envelope id %q does not match any active dispatch in the parallel block.", id),
		ID:       id,
		Proposal: "Ensure each wave returns the --id it was dispatched with.",
	}
}

// escDuplicate reports two envelopes correlating to the same id inside one
// parallel block (belt for §3 rule 5).
func escDuplicate(id string) *Escalation {
	return &Escalation{
		Class:    'B',
		Title:    "duplicate envelope",
		Detail:   fmt.Sprintf("two envelopes correlate to id %q in the same parallel block.", id),
		ID:       id,
		Proposal: "Ensure dispatch ids are unique within the parallel block.",
	}
}

// escParallelErrors reports one or more error returns inside a parallel
// block with no each -> {} handler (§6.8).
func escParallelErrors(ids []string) *Escalation {
	return &Escalation{
		Class:    'B',
		Title:    "unhandled parallel errors",
		Detail:   fmt.Sprintf("dispatches returned error with no each -> {} handler: %s", strings.Join(ids, ", ")),
		Proposal: "Add an each -> {} handler to route parallel results.",
	}
}

// bindResult exposes $result.{id,status,payload} to a handler scope.
func bindResult(r *ram.RAM, env *envelope.Envelope) {
	m := map[string]ram.Value{
		"id":      {Kind: ram.KString, Str: env.ID},
		"status":  {Kind: ram.KString, Str: string(env.Status)},
		"payload": payloadValue(env.Payload),
	}
	_ = r.Set("result", ram.Value{Kind: ram.KMap, Map: m}) // 'result' is valid
}

// payloadValue converts a typed envelope payload (string, list or object)
// into a RAM value. A nil payload binds an empty map.
func payloadValue(payload any) ram.Value {
	if payload == nil {
		return ram.Value{Kind: ram.KMap, Map: map[string]ram.Value{}}
	}
	return anyToValue(payload)
}

// anyToValue converts a decoded YAML value into a RAM value.
func anyToValue(v any) ram.Value {
	switch x := v.(type) {
	case string:
		return ram.Value{Kind: ram.KString, Str: x}
	case bool:
		return ram.Value{Kind: ram.KString, Str: strconv.FormatBool(x)}
	case int:
		return ram.Value{Kind: ram.KString, Str: strconv.Itoa(x)}
	case int64:
		return ram.Value{Kind: ram.KString, Str: strconv.FormatInt(x, 10)}
	case float64:
		return ram.Value{Kind: ram.KString, Str: strconv.FormatFloat(x, 'g', -1, 64)}
	case map[string]any:
		return mapToValue(x)
	case []any:
		return sliceToValue(x)
	case nil:
		return ram.Value{Kind: ram.KString}
	default:
		return ram.Value{Kind: ram.KString, Str: fmt.Sprint(x)}
	}
}

func mapToValue(m map[string]any) ram.Value {
	out := make(map[string]ram.Value, len(m))
	for k, v := range m {
		out[k] = anyToValue(v)
	}
	return ram.Value{Kind: ram.KMap, Map: out}
}

func sliceToValue(s []any) ram.Value {
	out := make([]ram.Value, 0, len(s))
	for _, v := range s {
		out = append(out, anyToValue(v))
	}
	return ram.Value{Kind: ram.KList, List: out}
}

// renderPayload renders a wave payload as a markdown report section.
func renderPayload(env *envelope.Envelope) string {
	y, err := yaml.Marshal(env.Payload)
	if err != nil {
		y = []byte(fmt.Sprintf("%v\n", env.Payload)) // payload is a plain map; fallback only
	}
	return fmt.Sprintf("## [%s] %s\n\n```yaml\n%s```\n\n", env.ID, env.Status, y)
}

// renderValue renders a RAM value emitted by [return] as a report block:
// strings verbatim, lists/maps as a fenced YAML block. An empty label (a
// single unlabeled return) emits the content with no "## " heading.
func renderValue(label string, v ram.Value) string {
	header := ""
	if label != "" {
		header = "## " + label + "\n\n"
	}
	if v.Kind == ram.KString {
		body := v.Str
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		return header + body + "\n"
	}
	y, err := yaml.Marshal(valueToAny(v))
	if err != nil {
		y = []byte(fmt.Sprintf("%v\n", valueToAny(v))) // value is a plain tree; fallback only
	}
	return header + "```yaml\n" + string(y) + "```\n\n"
}

// valueToAny converts a RAM value back into a plain tree for YAML marshaling
// (inverse of anyToValue).
func valueToAny(v ram.Value) any {
	switch v.Kind {
	case ram.KList:
		out := make([]any, len(v.List))
		for i, e := range v.List {
			out[i] = valueToAny(e)
		}
		return out
	case ram.KMap:
		out := make(map[string]any, len(v.Map))
		for k, e := range v.Map {
			out[k] = valueToAny(e)
		}
		return out
	default:
		return v.Str
	}
}
