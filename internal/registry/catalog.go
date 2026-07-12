package registry

import (
	"fmt"
	"sort"
	"strings"
)

// Catalog renders the human-readable capability catalog for the valid agents,
// sorted by command token. onlyAgent (bare "echo" or bracketed "[echo]") limits
// the catalog to that single agent; an empty onlyAgent renders the whole pool.
// The bool is false only when onlyAgent names an agent that is not registered.
func (r *Registry) Catalog(onlyAgent string) (string, bool) {
	names := r.Names()
	if onlyAgent != "" {
		cmd := onlyAgent
		if !strings.HasPrefix(cmd, "[") {
			cmd = "[" + cmd + "]"
		}
		if _, ok := r.agents[cmd]; !ok {
			return "", false
		}
		names = []string{cmd}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "AGENT CATALOG (%d agent(s))\n", len(names))
	for _, name := range names {
		b.WriteString("\n")
		b.WriteString(renderAgentCard(name, r.agents[name]))
	}
	return b.String(), true
}

// renderAgentCard renders one agent's calling card. name is the bracketed
// command token. Optional fields (inputs/produces/examples) are omitted when
// empty; operations are listed in sorted order for determinism.
func renderAgentCard(name string, a *Agent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  model: %s · scope: %s\n", name, a.Model, a.Scope)
	if c := a.Capabilities; c != nil {
		fmt.Fprintf(&b, "  purpose:   %s\n", c.Purpose)
		fmt.Fprintf(&b, "  use when:  %s\n", c.UseWhen)
		if c.Inputs != "" {
			fmt.Fprintf(&b, "  inputs:    %s\n", c.Inputs)
		}
		if c.Produces != "" {
			fmt.Fprintf(&b, "  produces:  %s\n", c.Produces)
		}
	}
	b.WriteString("  operations:\n")
	for _, opName := range sortedOperationNames(a.Operations) {
		op := a.Operations[opName]
		grants := "—"
		if len(op.Grants) > 0 {
			grants = strings.Join(op.Grants, ", ")
		}
		fmt.Fprintf(&b, "    %s — %s  [grants: %s]\n", opName, op.Description, grants)
	}
	if c := a.Capabilities; c != nil && len(c.Examples) > 0 {
		b.WriteString("  examples:\n")
		for _, ex := range c.Examples {
			fmt.Fprintf(&b, "    %s\n", ex)
		}
	}
	return b.String()
}

// sortedOperationNames returns the operation keys in deterministic order.
func sortedOperationNames(ops map[string]Operation) []string {
	names := make([]string, 0, len(ops))
	for name := range ops {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
