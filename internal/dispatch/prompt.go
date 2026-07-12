package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"arkannie/internal/registry"
	"arkannie/internal/template"
)

// AssemblePrompt renders the final wave prompt: the harness with the
// context_block, dispatch id, and the pre/post directive blocks resolved into
// their slots. Directive flags (groups, personality, modifiers) flow through
// pre/post; personality is no longer a separate prepended file.
func AssemblePrompt(a *registry.Agent, contextBlock, pre, post, dispatchID string) string {
	return template.Render(a.Harness, map[string]string{
		"context_block":   contextBlock,
		"id":              dispatchID,
		"directives_pre":  pre,
		"directives_post": post,
	})
}

// RenderDirectives builds the pre-context and post-context directive blocks
// from a resolved dispatch. pre = each active group's tag+text (sorted by
// group) followed by #personality (always, when the agent declares one);
// post = a single #modifiers tag with the present modifiers' text (sorted).
func RenderDirectives(a *registry.Agent, op *registry.Operation, res *FlagResolution) (pre, post string) {
	var b strings.Builder
	for _, group := range sortedKeys(res.Groups) {
		option := res.Groups[group]
		b.WriteString("#" + group + "\n")
		b.WriteString(op.Groups[group][option])
		b.WriteString("\n")
	}
	if a.Personality != nil {
		text := a.Personality.Default
		if res.Personality != "" {
			text = a.Personality.Values[res.Personality]
		}
		b.WriteString("#personality\n")
		b.WriteString(text)
		b.WriteString("\n")
	}
	pre = b.String()

	if len(res.Modifiers) > 0 {
		sorted := append([]string(nil), res.Modifiers...)
		sort.Strings(sorted)
		var m strings.Builder
		m.WriteString("#modifiers\n")
		for _, name := range sorted {
			m.WriteString(op.Modifiers[name])
			m.WriteString("\n")
		}
		post = m.String()
	}
	return pre, post
}

// MaterializeRunDir creates the ephemeral run directory
// <memDir>/runs/<runID>/<sanitized dispatchID>/ and writes prompt.md with
// the exact prompt content. It returns the directory path.
func MaterializeRunDir(memDir, runID, dispatchID, prompt string) (string, error) {
	runDir := filepath.Join(memDir, "runs", runID, sanitizeDispatchID(dispatchID))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("creating run dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "prompt.md"), []byte(prompt), 0o644); err != nil {
		return "", fmt.Errorf("writing prompt.md: %w", err)
	}
	return runDir, nil
}

// sanitizeDispatchID lowercases the id and maps every byte outside
// [a-z0-9-] to '-', which also keeps the result path-safe.
func sanitizeDispatchID(id string) string {
	var b strings.Builder
	b.Grow(len(id))
	for _, c := range strings.ToLower(id) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}
