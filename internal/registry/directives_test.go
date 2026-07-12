package registry

import (
	"os"
	"path/filepath"
	"testing"
)

// slotHarness carries both directive slots so directive-declaring fixtures
// pass VAL-16 and only the rule under test fires.
const slotHarness = "body\n{{ directives_pre }}\n## Dispatch\n{{ context_block }}\nid {{ id }}\n{{ directives_post }}\n"

func writeAgentHarness(t *testing.T, root, name, yamlSrc, harness string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(yamlSrc), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harness.md"), []byte(harness), 0o644); err != nil {
		t.Fatalf("write harness: %v", err)
	}
}

const directivesYAML = `command: "[echo]"
model: haiku
scope: agnostic
personality:
  default: "Neutral."
  values:
    techlead: "Think like a tech lead."
    coach: "Guide with questions."
operations:
  echo:
    id: echo-op
    description: Echo.
    flags:
      verbose: {type: boolean}
    groups:
      direction:
        backwards: "Reverse the output."
        forward: "Literal order."
    modifiers:
      terse: "Be concise."
    output_schema:
      success: {echo: string}
      error:
        reason: string
        recoverable: boolean
`

// TestLoadDirectives covers T1 (R1, R2): parsing and index construction.
func TestLoadDirectives(t *testing.T) {
	af, err := parseAgentFile([]byte(directivesYAML))
	if err != nil {
		t.Fatalf("parseAgentFile: %v", err)
	}
	a := buildAgent("/tmp/echo", []byte(directivesYAML), af, slotHarness)

	if a.Personality == nil || a.Personality.Default != "Neutral." {
		t.Fatalf("Personality = %+v, want Default 'Neutral.'", a.Personality)
	}
	if a.Personality.Values["techlead"] == "" {
		t.Error("Personality.Values[techlead] missing")
	}
	op := a.Operations["echo"]
	if op.Groups["direction"]["backwards"] == "" {
		t.Error("Groups[direction][backwards] missing")
	}
	if op.Modifiers["terse"] == "" {
		t.Error("Modifiers[terse] missing")
	}
	// R2: index option -> group.
	if op.optionGroups["backwards"] != "direction" || op.optionGroups["forward"] != "direction" {
		t.Errorf("optionGroups = %v, want backwards/forward -> direction", op.optionGroups)
	}
	if !op.modifierSet["terse"] {
		t.Error("modifierSet[terse] not set")
	}
}

// TestClassifyFlag covers T3 (R7).
func TestClassifyFlag(t *testing.T) {
	af, _ := parseAgentFile([]byte(directivesYAML))
	op := buildAgent("/tmp/echo", []byte(directivesYAML), af, slotHarness).Operations["echo"]

	tests := []struct {
		name      string
		wantRole  FlagRole
		wantGroup string
	}{
		{"backwards", RoleGroupOption, "direction"},
		{"forward", RoleGroupOption, "direction"},
		{"terse", RoleModifier, ""},
		{"verbose", RoleData, ""},
		{"nope", RoleUnknown, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role, group := op.ClassifyFlag(tt.name)
			if role != tt.wantRole || group != tt.wantGroup {
				t.Errorf("ClassifyFlag(%q) = (%d, %q), want (%d, %q)", tt.name, role, group, tt.wantRole, tt.wantGroup)
			}
		})
	}
}

// TestValidateDirectives covers T2 (R3, R4, R5, R6): VAL-13..16.
func TestValidateDirectives(t *testing.T) {
	base := func(opBody, personality string) string {
		return "command: \"[bad]\"\nmodel: haiku\nscope: agnostic\n" + personality +
			"capabilities:\n  purpose: Run.\n  use_when: Only in tests.\n" +
			"operations:\n  run:\n    id: run-op\n    description: Run.\n" + opBody +
			"    output_schema:\n      success: {}\n      error:\n        reason: string\n        recoverable: boolean\n"
	}
	tests := []struct {
		name     string
		yamlSrc  string
		harness  string
		wantRule string
	}{
		{
			name:     "VAL-13_option_in_two_groups",
			harness:  slotHarness,
			wantRule: "VAL-13",
			yamlSrc:  base("    groups:\n      a:\n        dup: \"x\"\n      b:\n        dup: \"y\"\n", ""),
		},
		{
			name:     "VAL-13_modifier_equals_option",
			harness:  slotHarness,
			wantRule: "VAL-13",
			yamlSrc:  base("    groups:\n      a:\n        shared: \"x\"\n    modifiers:\n      shared: \"y\"\n", ""),
		},
		{
			name:     "VAL-13_option_equals_operation_name",
			harness:  slotHarness,
			wantRule: "VAL-13",
			yamlSrc:  base("    groups:\n      a:\n        run: \"x\"\n", ""),
		},
		{
			name:     "VAL-14_personality_no_default",
			harness:  slotHarness,
			wantRule: "VAL-14",
			yamlSrc:  base("", "personality:\n  values:\n    x: \"t\"\n"),
		},
		{
			name:     "VAL-14_personality_empty_values",
			harness:  slotHarness,
			wantRule: "VAL-14",
			yamlSrc:  base("", "personality:\n  default: \"base\"\n  values: {}\n"),
		},
		{
			name:     "VAL-15_empty_option_text",
			harness:  slotHarness,
			wantRule: "VAL-15",
			yamlSrc:  base("    groups:\n      a:\n        opt: \"\"\n", ""),
		},
		{
			name:     "VAL-15_empty_modifier_text",
			harness:  slotHarness,
			wantRule: "VAL-15",
			yamlSrc:  base("    modifiers:\n      terse: \"\"\n", ""),
		},
		{
			name:     "VAL-16_groups_without_slot",
			harness:  "no slots here\n{{ context_block }}\n",
			wantRule: "VAL-16",
			yamlSrc:  base("    groups:\n      a:\n        opt: \"x\"\n", ""),
		},
		{
			name:     "valid_directives_pass",
			harness:  slotHarness,
			wantRule: "",
			yamlSrc:  base("    groups:\n      a:\n        opt: \"x\"\n    modifiers:\n      terse: \"y\"\n", "personality:\n  default: \"base\"\n  values:\n    z: \"t\"\n"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAgentHarness(t, dir, "bad", tt.yamlSrc, tt.harness)
			_, errs := Load(dir)
			if tt.wantRule == "" {
				if len(errs) != 0 {
					t.Fatalf("Load errors = %v, want none", errs)
				}
				return
			}
			if !hasErrorContaining(errs, tt.wantRule) {
				t.Errorf("Load errors = %v, want one naming %s", errs, tt.wantRule)
			}
		})
	}
}
