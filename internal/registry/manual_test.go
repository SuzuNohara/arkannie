package registry

import (
	"strings"
	"testing"
)

func TestManualPool(t *testing.T) {
	reg := catalogRegistry(t)
	out, ok := reg.Manual("")
	if !ok {
		t.Fatal(`Manual("") ok=false, want true`)
	}
	for _, want := range []string{
		"# [card] — manual",
		"# [good] — manual",
		"## Dispatch",
		"## Overview",
		"- **purpose:** Turn a raw requirement into a structured brief.",
		"## Operation `run`",
		"**grants:** read",
		"## Personalities",
		"## Ask Protocol & trust boundary",
		"## Examples",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("manual missing %q\n---\n%s", want, out)
		}
	}
	// Sorted by command token: [card] before [good].
	if strings.Index(out, "[card]") > strings.Index(out, "[good]") {
		t.Errorf("manual not sorted by command:\n%s", out)
	}
}

func TestManualDeterministic(t *testing.T) {
	reg := catalogRegistry(t)
	a, _ := reg.Manual("")
	b, _ := reg.Manual("")
	if a != b {
		t.Errorf("manual not deterministic:\n---A---\n%s\n---B---\n%s", a, b)
	}
}

func TestManualSingleAgent(t *testing.T) {
	reg := catalogRegistry(t)
	out, ok := reg.Manual("card")
	if !ok {
		t.Fatal(`Manual("card") ok=false, want true`)
	}
	if !strings.Contains(out, "[card]") {
		t.Errorf("single-agent manual missing [card]:\n%s", out)
	}
	if strings.Contains(out, "[good]") {
		t.Errorf("single-agent manual should not contain [good]:\n%s", out)
	}
	// Bracketed form resolves to the same manual.
	brk, ok := reg.Manual("[card]")
	if !ok || brk != out {
		t.Error(`Manual("[card]") != Manual("card")`)
	}
}

func TestManualNotFound(t *testing.T) {
	reg := catalogRegistry(t)
	if _, ok := reg.Manual("nope"); ok {
		t.Error(`Manual("nope") ok=true, want false`)
	}
}

// TestManualAgnosticDispatch checks the scope-specific dispatch guidance and
// that a synthesized per-operation example is always present.
func TestManualAgnosticDispatch(t *testing.T) {
	reg := catalogRegistry(t)
	out, _ := reg.Manual("card")
	if !strings.Contains(out, "Agnostic: read-only") {
		t.Errorf("agnostic dispatch rule missing:\n%s", out)
	}
	if !strings.Contains(out, "[card] --id=demo") {
		t.Errorf("synthesized per-op example missing:\n%s", out)
	}
}

// TestManualRichOperation renders an agent that exercises every per-operation
// section — flags, groups, modifiers, personalities and a typed success schema.
func TestManualRichOperation(t *testing.T) {
	af, err := parseAgentFile([]byte(directivesYAML))
	if err != nil {
		t.Fatalf("parseAgentFile: %v", err)
	}
	a := buildAgent("/tmp/echo", []byte(directivesYAML), af, slotHarness)
	out := renderAgentManual("[echo]", a)
	for _, want := range []string{
		"**flags:**",
		"`--verbose` (boolean)",
		"**groups (mutually-exclusive options):**",
		"`direction`: backwards, forward",
		"**modifiers (combinable):** `--terse`",
		"Select a lens with `--personality=<value>`:",
		"- `techlead`",
		"- `coach`",
		"success: `{echo: string}`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rich manual missing %q\n---\n%s", want, out)
		}
	}
}

// TestManualExecutorLayer covers the executor dispatch guidance and the layer
// origin/consent note.
func TestManualExecutorLayer(t *testing.T) {
	a := &Agent{
		Model:      "opus",
		Scope:      "executor",
		Layer:      &Layer{Origin: "/home/x/nova"},
		Operations: map[string]Operation{},
	}
	out := renderAgentManual("[layered]", a)
	if !strings.Contains(out, "Executor: requires `--allow-workspace`") {
		t.Errorf("executor dispatch rule missing:\n%s", out)
	}
	if !strings.Contains(out, "Layer agent** — origin `/home/x/nova`") {
		t.Errorf("layer origin note missing:\n%s", out)
	}
}
