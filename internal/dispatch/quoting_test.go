package dispatch

import (
	"strings"
	"testing"

	"arkannie/internal/ann"
	"arkannie/internal/ram"
)

// TestContextBlockEscapedDollar covers §quoting U1-T3/T4/T5 at the dispatch
// layer: an escaped \$ in the context text renders as a literal '$', is never
// resolved against RAM, and never raises the §7.3 Class B unresolvable error.
func TestContextBlockEscapedDollar(t *testing.T) {
	a := loadFixtureAgent(t)

	t.Run("U1-T3_escaped_dollar_literal_golden", func(t *testing.T) {
		r := ram.New()
		if err := r.Set("module", ram.Value{Kind: ram.KString, Str: "auth"}); err != nil {
			t.Fatal(err)
		}
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"target": "src"},
			Context: `price \$5 for $module`,
		}
		want := `operation: analyze
context:
  text: price $5 for auth
  target: src
flags:
  - format=yaml
  - target=src
` + analyzeSchemaGolden
		if got := buildBlock(t, a, d, r); got != want {
			t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("U1-T4_escaped_dollar_of_undefined_name_no_classB", func(t *testing.T) {
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"target": "src"},
			Context: `no such \$missing binding`,
		}
		op, name := mustSelect(t, a, d)
		got, err := BuildContextBlock(op, name, d, ram.New())
		if err != nil {
			t.Fatalf("escaped \\$missing must not raise Class B: %v", err)
		}
		if !strings.Contains(got, "$missing") || strings.Contains(got, `\$`) {
			t.Fatalf("literal $missing must appear (backslash stripped):\n%s", got)
		}
	})

	t.Run("U1-T5_multiline_escaped_dollar", func(t *testing.T) {
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"target": "src"},
			Context: "line one\n\ncost \\$5",
		}
		op, name := mustSelect(t, a, d)
		got, err := BuildContextBlock(op, name, d, ram.New())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, "$5") || strings.Contains(got, `\$`) {
			t.Fatalf("escaped dollar not restored in multiline text:\n%s", got)
		}
	})

	t.Run("U1-T8_regression_real_ref_still_resolves", func(t *testing.T) {
		r := ram.New()
		if err := r.Set("module", ram.Value{Kind: ram.KString, Str: "auth"}); err != nil {
			t.Fatal(err)
		}
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"target": "src"},
			Context: "review $module now",
		}
		got := buildBlock(t, a, d, r)
		if !strings.Contains(got, "review auth now") {
			t.Fatalf("real $ref must still resolve:\n%s", got)
		}
	})
}
