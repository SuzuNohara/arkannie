package dispatch

import (
	"os"
	"path/filepath"
	"testing"

	"arkannie/internal/ann"
	"arkannie/internal/registry"
)

const echoDirectivesYAML = `command: "[echo]"
model: haiku
scope: agnostic
default_operation: echo
capabilities:
  purpose: Echo.
  use_when: Only in tests.
personality:
  default: "Neutral."
  values:
    techlead: "Think like a tech lead."
operations:
  echo:
    id: echo-op
    description: Echo.
    flags:
      verbose: {type: boolean}
    groups:
      direction:
        backwards: "Reverse."
        forward: "Literal."
      casing:
        upper: "Uppercase."
    modifiers:
      terse: "Be concise."
    output_schema:
      success: {echo: string}
      error:
        reason: string
        recoverable: boolean
`

func loadEchoOp(t *testing.T) (*registry.Agent, *registry.Operation) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "echo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	harness := "wave {{ id }}\n{{ directives_pre }}{{ context_block }}\n{{ directives_post }}"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(echoDirectivesYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harness.md"), []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, errs := registry.Load(root)
	if len(errs) > 0 {
		t.Fatalf("load: %v", errs)
	}
	a, ok := reg.Resolve("echo")
	if !ok {
		t.Fatal("echo not registered")
	}
	op := a.Operations["echo"]
	return a, &op
}

// TestResolveFlags covers T4 (R7, R8, R9, R10, R11).
func TestResolveFlags(t *testing.T) {
	a, op := loadEchoOp(t)
	resolve := func(flags map[string]string) (*FlagResolution, error) {
		return ResolveFlags(a, op, "echo", &ann.Dispatch{Command: "echo", Flags: flags})
	}

	t.Run("classify_and_consume", func(t *testing.T) {
		res, err := resolve(map[string]string{"verbose": "", "backwards": "", "terse": "", "personality": "techlead"})
		if err != nil {
			t.Fatalf("ResolveFlags: %v", err)
		}
		if res.Groups["direction"] != "backwards" {
			t.Errorf("Groups[direction] = %q, want backwards", res.Groups["direction"])
		}
		if res.Personality != "techlead" {
			t.Errorf("Personality = %q, want techlead", res.Personality)
		}
		if len(res.Modifiers) != 1 || res.Modifiers[0] != "terse" {
			t.Errorf("Modifiers = %v, want [terse]", res.Modifiers)
		}
		// R11: directive flags consumed; only data remains.
		if _, ok := res.Data.Flags["verbose"]; !ok {
			t.Error("data flag verbose missing from Data")
		}
		for _, gone := range []string{"backwards", "terse", "personality"} {
			if _, ok := res.Data.Flags[gone]; ok {
				t.Errorf("directive flag %q leaked into Data.Flags", gone)
			}
		}
	})

	t.Run("two_groups_combine", func(t *testing.T) {
		res, err := resolve(map[string]string{"backwards": "", "upper": ""})
		if err != nil {
			t.Fatalf("ResolveFlags: %v", err)
		}
		if res.Groups["direction"] != "backwards" || res.Groups["casing"] != "upper" {
			t.Errorf("Groups = %v, want both", res.Groups)
		}
	})

	fail := []struct {
		name  string
		flags map[string]string
	}{
		{"same_group_exclusive", map[string]string{"backwards": "", "forward": ""}},
		{"group_option_with_value", map[string]string{"backwards": "x"}},
		{"modifier_with_value", map[string]string{"terse": "x"}},
		{"unknown_flag", map[string]string{"nope": ""}},
		{"personality_bad_value", map[string]string{"personality": "bogus"}},
		{"personality_no_value", map[string]string{"personality": ""}},
	}
	for _, tt := range fail {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resolve(tt.flags); err == nil {
				t.Errorf("ResolveFlags(%v) = nil error, want Class B", tt.flags)
			}
		})
	}
}
