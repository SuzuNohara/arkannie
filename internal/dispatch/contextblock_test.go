package dispatch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arkannie/internal/ann"
	"arkannie/internal/ram"
	"arkannie/internal/registry"
)

// fixtureAgentYAML is a registry fixture written to t.TempDir() so that
// OutputSchema carries the real verbatim block produced by registry.Load.
const fixtureAgentYAML = `command: "[reviewer]"
model: sonnet
scope: agnostic
default_operation: analyze
capabilities:
  purpose: Analyze a target path.
  use_when: Only in tests.

operations:
  analyze:
    id: analyze-op
    description: Analyze a target path.
    context:
      text:
        type: string
        required: false
      target:
        type: string
        required: true
      depth:
        type: string
        required: false
    flags:
      target:
        type: string
        required: true
      depth:
        type: string
      verbose:
        type: boolean
      format:
        type: string
        default: yaml
    grants: [read]
    output_schema:
      success:
        findings: string
        summary: string
      error:
        reason: string
        recoverable: boolean
      info:
        message: string
  new:
    id: new-op
    description: Start a fresh review session.
    output_schema:
      success:
        ok: string
      error:
        reason: string
        recoverable: boolean
`

const analyzeSchemaGolden = `output_schema: |
  success:
    findings: string
    summary: string
  error:
    reason: string
    recoverable: boolean
  info:
    message: string
`

func loadFixtureAgent(t *testing.T) *registry.Agent {
	t.Helper()
	agentsDir := t.TempDir()
	dir := filepath.Join(agentsDir, "reviewer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "agent.yaml"), fixtureAgentYAML)
	writeTestFile(t, filepath.Join(dir, "harness.md"), "{{ context_block }}\n\nid: {{ id }}\n")
	reg, errs := registry.Load(agentsDir)
	if len(errs) > 0 {
		t.Fatalf("fixture registry errors: %v", errs)
	}
	a, ok := reg.Resolve("reviewer")
	if !ok {
		t.Fatal("fixture agent [reviewer] not resolved")
	}
	return a
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustSelect(t *testing.T, a *registry.Agent, d *ann.Dispatch) (*registry.Operation, string) {
	t.Helper()
	op, name, err := SelectOperation(a, d)
	if err != nil {
		t.Fatalf("SelectOperation: %v", err)
	}
	return op, name
}

func wantClassB(t *testing.T, err error) *PreDispatchError {
	t.Helper()
	var pde *PreDispatchError
	if !errors.As(err, &pde) {
		t.Fatalf("want *PreDispatchError, got %v (%T)", err, err)
	}
	if pde.Class != 'B' {
		t.Fatalf("want Class 'B', got %q (msg: %s)", pde.Class, pde.Msg)
	}
	return pde
}

func TestSelectOperation(t *testing.T) {
	a := loadFixtureAgent(t)

	t.Run("U7-T2_flag_selects_operation", func(t *testing.T) {
		op, name := mustSelect(t, a, &ann.Dispatch{Command: "reviewer", Flags: map[string]string{"new": ""}})
		if name != "new" || op.ID != "new-op" {
			t.Fatalf("want operation new/new-op, got %s/%s", name, op.ID)
		}
	})

	t.Run("U7-T2_two_operation_flags_error_B", func(t *testing.T) {
		_, _, err := SelectOperation(a, &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"new": "", "analyze": ""},
		})
		wantClassB(t, err)
	})

	t.Run("U7-T2_default_operation", func(t *testing.T) {
		op, name := mustSelect(t, a, &ann.Dispatch{Command: "reviewer"})
		if name != "analyze" || op.ID != "analyze-op" {
			t.Fatalf("want operation analyze/analyze-op, got %s/%s", name, op.ID)
		}
	})

	t.Run("U7-T2_no_selector_no_default_error_B_lists_ops", func(t *testing.T) {
		noDefault := *a
		noDefault.DefaultOperation = ""
		_, _, err := SelectOperation(&noDefault, &ann.Dispatch{Command: "reviewer"})
		pde := wantClassB(t, err)
		for _, want := range []string{"analyze", "new"} {
			if !strings.Contains(pde.Msg, want) {
				t.Fatalf("error message %q must list operation %q", pde.Msg, want)
			}
		}
	})
}

func buildBlock(t *testing.T, a *registry.Agent, d *ann.Dispatch, r *ram.RAM) string {
	t.Helper()
	op, name := mustSelect(t, a, d)
	got, err := BuildContextBlock(op, name, d, r)
	if err != nil {
		t.Fatalf("BuildContextBlock: %v", err)
	}
	return got
}

func TestBuildContextBlock(t *testing.T) {
	a := loadFixtureAgent(t)

	t.Run("U7-T1_full_dispatch_golden", func(t *testing.T) {
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags: map[string]string{
				"target": "src/auth", "depth": "full", "verbose": "",
				"id": "rev-1", "timeout": "30",
			},
			Context: "analyze the auth module",
			ID:      "rev-1",
		}
		want := `operation: analyze
context:
  text: analyze the auth module
  depth: full
  target: src/auth
flags:
  - depth=full
  - format=yaml
  - target=src/auth
  - verbose
` + analyzeSchemaGolden
		if got := buildBlock(t, a, d, ram.New()); got != want {
			t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("U7-T3_kstring_binding_inline", func(t *testing.T) {
		r := ram.New()
		if err := r.Set("module", ram.Value{Kind: ram.KString, Str: "auth"}); err != nil {
			t.Fatal(err)
		}
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"target": "src"},
			Context: "review $module now",
		}
		want := `operation: analyze
context:
  text: review auth now
  target: src
flags:
  - format=yaml
  - target=src
` + analyzeSchemaGolden
		if got := buildBlock(t, a, d, r); got != want {
			t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("U7-T3_klist_binding_as_context_field", func(t *testing.T) {
		r := ram.New()
		items := ram.Value{Kind: ram.KList, List: []ram.Value{
			{Kind: ram.KString, Str: "alpha"},
			{Kind: ram.KString, Str: "beta"},
		}}
		if err := r.Set("items", items); err != nil {
			t.Fatal(err)
		}
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"target": "src"},
			Context: "process $items",
		}
		want := `operation: analyze
context:
  text: process items
  items:
    - alpha
    - beta
  target: src
flags:
  - format=yaml
  - target=src
` + analyzeSchemaGolden
		if got := buildBlock(t, a, d, r); got != want {
			t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("U7-T3_kmap_binding_as_context_field", func(t *testing.T) {
		r := ram.New()
		meta := ram.Value{Kind: ram.KMap, Map: map[string]ram.Value{
			"owner": {Kind: ram.KString, Str: "suzu"},
			"team":  {Kind: ram.KString, Str: "core"},
		}}
		if err := r.Set("meta", meta); err != nil {
			t.Fatal(err)
		}
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"target": "src"},
			Context: "use $meta",
		}
		want := `operation: analyze
context:
  text: use meta
  meta:
    owner: suzu
    team: core
  target: src
flags:
  - format=yaml
  - target=src
` + analyzeSchemaGolden
		if got := buildBlock(t, a, d, r); got != want {
			t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("U7-T4_unresolvable_binding_error_B", func(t *testing.T) {
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"target": "src"},
			Context: "check $missing",
		}
		op, name := mustSelect(t, a, d)
		_, err := BuildContextBlock(op, name, d, ram.New())
		pde := wantClassB(t, err)
		if !strings.Contains(pde.Msg, "$missing") {
			t.Fatalf("error %q must name the unresolvable binding", pde.Msg)
		}
	})

	t.Run("U7-T4_required_field_without_value_error_B", func(t *testing.T) {
		d := &ann.Dispatch{Command: "reviewer", Context: "no target anywhere"}
		op, name := mustSelect(t, a, d)
		_, err := BuildContextBlock(op, name, d, ram.New())
		pde := wantClassB(t, err)
		if !strings.Contains(pde.Msg, "target") {
			t.Fatalf("error %q must name the missing required field", pde.Msg)
		}
	})

	t.Run("U7-T4_optional_field_without_value_omitted", func(t *testing.T) {
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"target": "src"},
			Context: "hello",
		}
		got := buildBlock(t, a, d, ram.New())
		if strings.Contains(got, "depth") {
			t.Fatalf("optional field depth without value must be omitted, got:\n%s", got)
		}
	})

	t.Run("U7-T4_unknown_flag_error_B", func(t *testing.T) {
		d := &ann.Dispatch{
			Command: "reviewer",
			Flags:   map[string]string{"target": "src", "bogus": "x"},
		}
		op, name := mustSelect(t, a, d)
		_, err := BuildContextBlock(op, name, d, ram.New())
		pde := wantClassB(t, err)
		if !strings.Contains(pde.Msg, "bogus") {
			t.Fatalf("error %q must name the unknown flag", pde.Msg)
		}
	})

	t.Run("U7-T5_empty_context_and_flags_golden", func(t *testing.T) {
		d := &ann.Dispatch{Command: "reviewer", Flags: map[string]string{"new": ""}}
		want := `operation: new
context: {}
flags: []
output_schema: |
  success:
    ok: string
  error:
    reason: string
    recoverable: boolean
`
		if got := buildBlock(t, a, d, ram.New()); got != want {
			t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("U7-T6_empty_output_schema_error_B", func(t *testing.T) {
		op := &registry.Operation{ID: "bad-op", Description: "no schema"}
		_, err := BuildContextBlock(op, "bad", &ann.Dispatch{Command: "bad"}, ram.New())
		wantClassB(t, err)
	})
}
