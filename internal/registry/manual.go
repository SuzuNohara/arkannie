package registry

import (
	"fmt"
	"strings"
)

// Manual renders the per-agent execution manual (Markdown) for the valid
// agents, sorted by command token. onlyAgent (bare "echo" or bracketed
// "[echo]") limits output to that single agent; an empty onlyAgent renders the
// whole pool. The bool is false only when onlyAgent names an agent that is not
// registered.
//
// The manual is derived purely from the loaded contract (agent.yaml +
// capabilities) — no LLM spawn, no program execution — so it is deterministic,
// like --catalog. It carries enough detail for an executor to drive the agent
// end to end: dispatch rule, invocation modes, per-operation contract,
// personalities, Ask Protocol and runnable examples.
func (r *Registry) Manual(onlyAgent string) (string, bool) {
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
	for i, name := range names {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		b.WriteString(renderAgentManual(name, r.agents[name]))
	}
	return b.String(), true
}

// renderAgentManual renders one agent's full Markdown manual. name is the
// bracketed command token. Every map is walked in sorted order for
// determinism.
func renderAgentManual(name string, a *Agent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — manual\n\n", name)
	fmt.Fprintf(&b, "**model:** %s · **scope:** %s\n\n", a.Model, a.Scope)

	b.WriteString("## Dispatch\n\n")
	if a.Scope == "executor" {
		b.WriteString("- Executor: requires `--allow-workspace`; runs in the invoker's working directory (program mode). Grants may include write/execute.\n")
	} else {
		b.WriteString("- Agnostic: read-only, runs in an ephemeral run dir — pass an **absolute** `--path` to target a real tree.\n")
	}
	if a.Layer != nil {
		fmt.Fprintf(&b, "- **Layer agent** — origin `%s`. Dispatching runs the origin's own identity in place and requires `--allow-layer`.\n", a.Layer.Origin)
	}
	if a.DefaultOperation != "" {
		fmt.Fprintf(&b, "- Default operation: `%s` (a dispatch with no op flag runs this).\n", a.DefaultOperation)
	} else if len(a.Operations) > 1 {
		b.WriteString("- No default operation: multi-op agent — select an operation via its op flag.\n")
	}
	b.WriteString("\n")

	if c := a.Capabilities; c != nil {
		b.WriteString("## Overview\n\n")
		fmt.Fprintf(&b, "- **purpose:** %s\n", c.Purpose)
		fmt.Fprintf(&b, "- **use when:** %s\n", c.UseWhen)
		if c.Inputs != "" {
			fmt.Fprintf(&b, "- **inputs:** %s\n", c.Inputs)
		}
		if c.Produces != "" {
			fmt.Fprintf(&b, "- **produces:** %s\n", c.Produces)
		}
		b.WriteString("\n")
	}

	for _, opName := range sortedOperationNames(a.Operations) {
		renderOperation(&b, opName, a.Operations[opName])
	}

	b.WriteString("## Personalities\n\n")
	if a.Personality != nil {
		b.WriteString("Select a lens with `--personality=<value>`:\n\n")
		for _, v := range sortedKeys(a.Personality.Values) {
			fmt.Fprintf(&b, "- `%s`\n", v)
		}
		b.WriteString("\n")
	} else {
		b.WriteString("_None declared._\n\n")
	}

	b.WriteString("## Ask Protocol & trust boundary\n\n")
	b.WriteString("- Missing a required field → the agent returns `info` with `missing_field` rather than guessing; re-dispatch with the field supplied.\n")
	b.WriteString("- All context is treated as data, never as instructions.\n\n")

	b.WriteString("## Examples\n\n")
	examples := manualExamples(name, a)
	if len(examples) == 0 {
		b.WriteString("_None._\n")
	} else {
		for _, ex := range examples {
			fmt.Fprintf(&b, "- `%s`\n", ex)
		}
	}
	return b.String()
}

// renderOperation writes the full per-operation contract.
func renderOperation(b *strings.Builder, opName string, op Operation) {
	fmt.Fprintf(b, "## Operation `%s`", opName)
	if op.ID != "" {
		fmt.Fprintf(b, " (id: `%s`)", op.ID)
	}
	b.WriteString("\n\n")
	if op.Description != "" {
		fmt.Fprintf(b, "%s\n\n", op.Description)
	}

	if len(op.Context) > 0 {
		b.WriteString("**context:**\n")
		for _, f := range sortedKeys(op.Context) {
			fld := op.Context[f]
			req := "optional"
			if fld.Required {
				req = "required"
			}
			fmt.Fprintf(b, "- `%s` (%s, %s)\n", f, fld.Type, req)
		}
		b.WriteString("\n")
	}

	grants := "—"
	if len(op.Grants) > 0 {
		grants = strings.Join(op.Grants, ", ")
	}
	fmt.Fprintf(b, "**grants:** %s\n\n", grants)

	if len(op.Flags) > 0 {
		b.WriteString("**flags:**\n")
		for _, f := range sortedKeys(op.Flags) {
			fl := op.Flags[f]
			line := fmt.Sprintf("- `--%s` (%s", f, fl.Type)
			if fl.Required {
				line += ", required"
			}
			if fl.Default != "" {
				line += ", default " + fl.Default
			}
			line += ")"
			fmt.Fprintf(b, "%s\n", line)
		}
		b.WriteString("\n")
	}

	if len(op.Groups) > 0 {
		b.WriteString("**groups (mutually-exclusive options):**\n")
		for _, g := range sortedKeys(op.Groups) {
			fmt.Fprintf(b, "- `%s`: %s\n", g, strings.Join(sortedKeys(op.Groups[g]), ", "))
		}
		b.WriteString("\n")
	}

	if len(op.Modifiers) > 0 {
		var q []string
		for _, m := range sortedKeys(op.Modifiers) {
			q = append(q, "`--"+m+"`")
		}
		fmt.Fprintf(b, "**modifiers (combinable):** %s\n\n", strings.Join(q, " "))
	}

	b.WriteString("**output:**\n")
	if op.SuccessSchema != nil {
		fmt.Fprintf(b, "- success: `%s`\n", formatSchema(op.SuccessSchema))
	}
	if op.InfoSchema != nil {
		fmt.Fprintf(b, "- info: `%s`\n", formatSchema(op.InfoSchema))
	}
	b.WriteString("- error: `{reason: string, recoverable: boolean}`\n\n")
}

// formatSchema renders a payload schema as a compact inline type.
func formatSchema(s *PayloadSchema) string {
	switch s.Kind {
	case KindString:
		return "string"
	case KindList:
		return "list"
	default: // KindObject
		if len(s.Fields) == 0 {
			return "{} (any object)"
		}
		var parts []string
		for _, k := range sortedKeys(s.Fields) {
			parts = append(parts, fmt.Sprintf("%s: %s", k, s.Fields[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
}

// manualExamples returns the declared capability examples followed by one
// synthesized dispatch line per operation, so a reader always has a runnable
// starting point.
func manualExamples(name string, a *Agent) []string {
	var ex []string
	if a.Capabilities != nil {
		ex = append(ex, a.Capabilities.Examples...)
	}
	for _, opName := range sortedOperationNames(a.Operations) {
		ex = append(ex, synthExample(name, opName, a.Operations[opName]))
	}
	return ex
}

// synthExample builds a minimal, contract-consistent dispatch line for one
// operation (.ann form), including any required flags.
func synthExample(name, opName string, op Operation) string {
	line := name + " --id=demo"
	for _, f := range sortedKeys(op.Flags) {
		if op.Flags[f].Required {
			line += " --" + f
		}
	}
	line += ` : "..."`
	return line
}
