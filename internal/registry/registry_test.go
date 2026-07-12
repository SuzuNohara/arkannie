package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAgent creates .agents-style fixture: <agentsDir>/<name>/agent.yaml
// (+ harness.md unless withHarness is false).
func writeAgent(t *testing.T, agentsDir, name, yamlSrc string, withHarness bool) {
	t.Helper()
	dir := filepath.Join(agentsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(yamlSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if !withHarness {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "harness.md"), []byte("harness body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

const validAgentYAML = `command: "[good]"
model: haiku
scope: agnostic
timeout: 30
default_operation: run
capabilities:
  purpose: Run the thing.
  use_when: Only in tests.
operations:
  run:
    id: run-op
    description: Run the thing.
    grants: [read]
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`

func hasErrorContaining(errs []error, substrs ...string) bool {
	for _, err := range errs {
		all := true
		for _, s := range substrs {
			if !strings.Contains(err.Error(), s) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func TestLoad(t *testing.T) {
	t.Run("U6-T1_valid_fixture_echo", func(t *testing.T) {
		reg, errs := Load(filepath.Join("..", "..", ".agents"))
		if len(errs) != 0 {
			t.Fatalf("Load(.agents) errors = %v, want none", errs)
		}
		agent, ok := reg.Resolve("[echo]")
		if !ok {
			t.Fatal("Resolve([echo]) = false, want registered agent")
		}
		if agent.Command != "[echo]" || agent.Model != "haiku" || agent.Scope != "agnostic" {
			t.Errorf("agent header = %q/%q/%q, want [echo]/haiku/agnostic",
				agent.Command, agent.Model, agent.Scope)
		}
		if agent.Timeout != 60 {
			t.Errorf("Timeout = %d, want 60", agent.Timeout)
		}
		if agent.DefaultOperation != "echo" {
			t.Errorf("DefaultOperation = %q, want echo", agent.DefaultOperation)
		}
		if !strings.HasSuffix(filepath.ToSlash(agent.Dir), ".agents/echo") {
			t.Errorf("Dir = %q, want .agents/echo", agent.Dir)
		}
		if !strings.Contains(agent.Harness, "wave agent") {
			t.Errorf("Harness not loaded, got %q", agent.Harness)
		}
		op, ok := agent.Operations["echo"]
		if !ok {
			t.Fatalf("operation echo missing, got %v", agent.Operations)
		}
		if op.ID != "echo-op" || op.Description == "" {
			t.Errorf("op id/description = %q/%q, want echo-op/non-empty", op.ID, op.Description)
		}
		if f := op.Context["text"]; f.Type != "string" || f.Required {
			t.Errorf("context.text = %+v, want {string false}", f)
		}
		if len(op.Grants) != 1 || op.Grants[0] != "read" {
			t.Errorf("Grants = %v, want [read]", op.Grants)
		}
		wantSchema := "success:\n" +
			"  echo: string\n" +
			"error:\n" +
			"  reason: string\n" +
			"  recoverable: boolean\n" +
			"info:\n" +
			"  message: string\n"
		if op.OutputSchema != wantSchema {
			t.Errorf("OutputSchema not verbatim:\ngot:\n%s\nwant:\n%s", op.OutputSchema, wantSchema)
		}
	})

	t.Run("U6-T3_empty_agents_dir", func(t *testing.T) {
		reg, errs := Load(t.TempDir())
		if len(errs) != 0 {
			t.Fatalf("Load(empty) errors = %v, want none", errs)
		}
		if n := reg.Names(); len(n) != 0 {
			t.Errorf("Names() = %v, want empty", n)
		}
	})

	t.Run("U6-T3_missing_agents_dir", func(t *testing.T) {
		reg, errs := Load(filepath.Join(t.TempDir(), "does-not-exist"))
		if len(errs) != 0 {
			t.Fatalf("Load(missing) errors = %v, want none", errs)
		}
		if n := reg.Names(); len(n) != 0 {
			t.Errorf("Names() = %v, want empty", n)
		}
	})

	t.Run("U6-T4_missing_harness", func(t *testing.T) {
		dir := t.TempDir()
		writeAgent(t, dir, "noharness", validAgentYAML, false)
		_, errs := Load(dir)
		if !hasErrorContaining(errs, "harness.md", "noharness") {
			t.Errorf("errors = %v, want harness.md required naming the agent", errs)
		}
	})

	// The file-based agent-level personality mechanism was replaced by the
	// inline personality flag ({default, values}); its parsing and validation
	// are covered by TestLoadDirectives / TestValidateDirectives.

	t.Run("U6-T6_resolve_with_and_without_brackets", func(t *testing.T) {
		dir := t.TempDir()
		writeAgent(t, dir, "good", validAgentYAML, true)
		reg, errs := Load(dir)
		if len(errs) != 0 {
			t.Fatalf("errors = %v, want none", errs)
		}
		if _, ok := reg.Resolve("good"); !ok {
			t.Error("Resolve(good) = false, want true")
		}
		if _, ok := reg.Resolve("[good]"); !ok {
			t.Error("Resolve([good]) = false, want true")
		}
		if _, ok := reg.Resolve("nope"); ok {
			t.Error("Resolve(nope) = true, want false")
		}
	})

	t.Run("U6-T7_duplicate_command_across_agents", func(t *testing.T) {
		dir := t.TempDir()
		writeAgent(t, dir, "alpha", validAgentYAML, true)
		writeAgent(t, dir, "beta", validAgentYAML, true)
		_, errs := Load(dir)
		if !hasErrorContaining(errs, "duplicate command", "[good]") {
			t.Errorf("errors = %v, want duplicate command error", errs)
		}
	})

	t.Run("missing_agent_yaml", func(t *testing.T) {
		dir := t.TempDir()
		adir := filepath.Join(dir, "empty")
		if err := os.MkdirAll(adir, 0o755); err != nil {
			t.Fatal(err)
		}
		_, errs := Load(dir)
		if !hasErrorContaining(errs, "agent.yaml", "empty") {
			t.Errorf("errors = %v, want agent.yaml required error", errs)
		}
	})

	t.Run("malformed_yaml", func(t *testing.T) {
		dir := t.TempDir()
		writeAgent(t, dir, "broken", "command: [unclosed\n\t bad", true)
		_, errs := Load(dir)
		if !hasErrorContaining(errs, "broken", "agent.yaml") {
			t.Errorf("errors = %v, want parse error naming the file", errs)
		}
	})

	t.Run("names_sorted", func(t *testing.T) {
		dir := t.TempDir()
		writeAgent(t, dir, "zeta", strings.Replace(validAgentYAML, "[good]", "[zeta]", 1), true)
		writeAgent(t, dir, "alpha", strings.Replace(validAgentYAML, "[good]", "[alpha]", 1), true)
		reg, errs := Load(dir)
		if len(errs) != 0 {
			t.Fatalf("errors = %v, want none", errs)
		}
		names := reg.Names()
		if len(names) != 2 || names[0] != "[alpha]" || names[1] != "[zeta]" {
			t.Errorf("Names() = %v, want [[alpha] [zeta]]", names)
		}
	})
}

// TestLoadLayerAgent covers the layer: contract key (VAL-17). Origins are
// absolute paths created via t.TempDir(); the arkannie root of each case is the
// parent of its .agents dir.
func TestLoadLayerAgent(t *testing.T) {
	withLayer := func(origin string) string {
		return validAgentYAML + "layer:\n  origin: \"" + origin + "\"\n"
	}
	makeOrigin := func(t *testing.T, withIdentity bool) string {
		t.Helper()
		dir := t.TempDir()
		if !withIdentity {
			return dir
		}
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# identity\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	tests := []struct {
		name string
		// fixture returns the agent.yaml source plus the origin expected on
		// the loaded agent ("" means Layer must be nil).
		fixture func(t *testing.T, root string) (yamlSrc, wantOrigin string)
		wantErr []string // substrings of one expected error; nil = agent loads
	}{
		{
			name: "RG1_no_layer_key_backward_compat",
			fixture: func(t *testing.T, root string) (string, string) {
				return validAgentYAML, ""
			},
		},
		{
			name: "RG2_valid_absolute_origin",
			fixture: func(t *testing.T, root string) (string, string) {
				origin := makeOrigin(t, true)
				return withLayer(origin), origin
			},
		},
		{
			name: "RG3_empty_origin",
			fixture: func(t *testing.T, root string) (string, string) {
				return validAgentYAML + "layer:\n  origin: \"\"\n", ""
			},
			wantErr: []string{"VAL-17", "layer.origin is required"},
		},
		{
			name: "RG3_missing_origin",
			fixture: func(t *testing.T, root string) (string, string) {
				return validAgentYAML + "layer: {}\n", ""
			},
			wantErr: []string{"VAL-17", "layer.origin is required"},
		},
		{
			name: "RG4_relative_origin",
			fixture: func(t *testing.T, root string) (string, string) {
				return withLayer("../other-ai"), ""
			},
			wantErr: []string{"VAL-17", "must be an absolute path"},
		},
		{
			name: "RG5_nonexistent_origin",
			fixture: func(t *testing.T, root string) (string, string) {
				return withLayer(filepath.Join(t.TempDir(), "does-not-exist")), ""
			},
			wantErr: []string{"VAL-17", "existing directory"},
		},
		{
			name: "RG5_origin_is_regular_file",
			fixture: func(t *testing.T, root string) (string, string) {
				f := filepath.Join(t.TempDir(), "file.txt")
				if err := os.WriteFile(f, []byte("not a dir\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return withLayer(f), ""
			},
			wantErr: []string{"VAL-17", "existing directory"},
		},
		{
			name: "RG6_origin_without_claude_md",
			fixture: func(t *testing.T, root string) (string, string) {
				return withLayer(makeOrigin(t, false)), ""
			},
			wantErr: []string{"VAL-17", "identity"},
		},
		{
			name: "RG7_origin_is_root",
			fixture: func(t *testing.T, root string) (string, string) {
				if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# identity\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return withLayer(root), ""
			},
			wantErr: []string{"VAL-17", "must not overlap"},
		},
		{
			name: "RG7_origin_inside_root",
			fixture: func(t *testing.T, root string) (string, string) {
				sub := filepath.Join(root, "inner")
				if err := os.MkdirAll(sub, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(sub, "CLAUDE.md"), []byte("# identity\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return withLayer(sub), ""
			},
			wantErr: []string{"VAL-17", "must not overlap"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			agentsDir := filepath.Join(root, ".agents")
			yamlSrc, wantOrigin := tc.fixture(t, root)
			writeAgent(t, agentsDir, "layered", yamlSrc, true)
			reg, errs := Load(agentsDir)
			if tc.wantErr != nil {
				if !hasErrorContaining(errs, tc.wantErr...) {
					t.Fatalf("errors = %v, want one containing %v", errs, tc.wantErr)
				}
				if _, ok := reg.Resolve("[good]"); ok {
					t.Error("invalid layer agent was registered, want excluded")
				}
				return
			}
			if len(errs) != 0 {
				t.Fatalf("errors = %v, want none", errs)
			}
			agent, ok := reg.Resolve("[good]")
			if !ok {
				t.Fatal("Resolve([good]) = false, want registered agent")
			}
			if wantOrigin == "" {
				if agent.Layer != nil {
					t.Errorf("Layer = %+v, want nil", agent.Layer)
				}
				return
			}
			if agent.Layer == nil || agent.Layer.Origin != wantOrigin {
				t.Errorf("Layer = %+v, want Origin %q", agent.Layer, wantOrigin)
			}
		})
	}

	t.Run("RG8_broken_layer_agent_does_not_poison_registry", func(t *testing.T) {
		root := t.TempDir()
		agentsDir := filepath.Join(root, ".agents")
		writeAgent(t, agentsDir, "broken", validAgentYAML+"layer:\n  origin: \"relative/path\"\n", true)
		writeAgent(t, agentsDir, "healthy", strings.Replace(validAgentYAML, "[good]", "[healthy]", 1), true)
		reg, errs := Load(agentsDir)
		if _, ok := reg.Resolve("[healthy]"); !ok {
			t.Error("Resolve([healthy]) = false, want healthy agent registered")
		}
		if _, ok := reg.Resolve("[good]"); ok {
			t.Error("broken layer agent was registered, want excluded")
		}
		if len(errs) == 0 {
			t.Fatal("errors = none, want VAL-17 error(s)")
		}
		for _, err := range errs {
			if !strings.Contains(err.Error(), "VAL-17") {
				t.Errorf("unexpected non-VAL-17 error: %v", err)
			}
		}
	})
}
