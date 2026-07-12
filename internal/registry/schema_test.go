package registry

import "testing"

// U6-T2: one violation per active validation rule. Each fixture is a complete
// agent.yaml that breaks exactly one rule. VAL-02 (command_type) is retired
// per spec/divergence-notes.md — an extra command_type field is NOT an error.
func TestValidationRules(t *testing.T) {
	tests := []struct {
		name     string
		yamlSrc  string
		wantRule string // "" means no errors expected
	}{
		{
			name:     "U6-T2_VAL-01_invalid_command",
			wantRule: "VAL-01",
			yamlSrc: `command: "[Bad_Name!]"
model: haiku
scope: agnostic
operations:
  run:
    id: run-op
    description: Run.
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
		{
			name:     "U6-T2_VAL-02_retired_command_type_is_not_an_error",
			wantRule: "",
			yamlSrc: `command: "[bad]"
command_type: wave
model: haiku
scope: agnostic
capabilities:
  purpose: Run.
  use_when: Only in tests.
operations:
  run:
    id: run-op
    description: Run.
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
		{
			name:     "U6-T2_VAL-03_no_operations",
			wantRule: "VAL-03",
			yamlSrc: `command: "[bad]"
model: haiku
scope: agnostic
operations: {}
`,
		},
		{
			name:     "U6-T2_VAL-04_operation_missing_description",
			wantRule: "VAL-04",
			yamlSrc: `command: "[bad]"
model: haiku
scope: agnostic
operations:
  run:
    id: run-op
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
		{
			name:     "U6-T2_VAL-05_output_schema_without_success",
			wantRule: "VAL-05",
			yamlSrc: `command: "[bad]"
model: haiku
scope: agnostic
operations:
  run:
    id: run-op
    description: Run.
    output_schema:
      error:
        reason: string
        recoverable: boolean
`,
		},
		{
			name:     "U6-T2_VAL-06_error_missing_recoverable",
			wantRule: "VAL-06",
			yamlSrc: `command: "[bad]"
model: haiku
scope: agnostic
operations:
  run:
    id: run-op
    description: Run.
    output_schema:
      success: {}
      error:
        reason: string
`,
		},
		{
			name:     "U6-T2_VAL-07_duplicate_operation_id",
			wantRule: "VAL-07",
			yamlSrc: `command: "[bad]"
model: haiku
scope: agnostic
operations:
  one:
    id: same-op
    description: First.
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
  two:
    id: same-op
    description: Second.
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
		{
			name:     "U6-T2_VAL-08_invalid_flag_type",
			wantRule: "VAL-08",
			yamlSrc: `command: "[bad]"
model: haiku
scope: agnostic
operations:
  run:
    id: run-op
    description: Run.
    flags:
      speed:
        type: float
        required: false
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
		{
			name:     "U6-T2_VAL-09_invalid_grant",
			wantRule: "VAL-09",
			yamlSrc: `command: "[bad]"
model: haiku
scope: agnostic
operations:
  run:
    id: run-op
    description: Run.
    grants: [sudo]
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
		{
			name:     "U6-T2_VAL-10_invalid_model",
			wantRule: "VAL-10",
			yamlSrc: `command: "[bad]"
model: gpt-4
scope: agnostic
operations:
  run:
    id: run-op
    description: Run.
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
		{
			name:     "U6-T2_VAL-11_invalid_scope",
			wantRule: "VAL-11",
			yamlSrc: `command: "[bad]"
model: haiku
scope: global
operations:
  run:
    id: run-op
    description: Run.
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
		{
			name:     "U6-T2_VAL-12_agnostic_with_write_grant",
			wantRule: "VAL-12",
			yamlSrc: `command: "[bad]"
model: haiku
scope: agnostic
operations:
  run:
    id: run-op
    description: Run.
    grants: [read, write]
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAgent(t, dir, "bad", tt.yamlSrc, true)
			_, errs := Load(dir)
			if tt.wantRule == "" {
				if len(errs) != 0 {
					t.Fatalf("Load errors = %v, want none (rule retired)", errs)
				}
				return
			}
			if !hasErrorContaining(errs, tt.wantRule, "agent.yaml") {
				t.Errorf("Load errors = %v, want one naming %s and the file", errs, tt.wantRule)
			}
		})
	}
}

// Extra structural rules from the brief (file + rule named in each error).
func TestExtraRules(t *testing.T) {
	tests := []struct {
		name    string
		yamlSrc string
		want    []string
	}{
		{
			name: "timeout_zero_is_error",
			want: []string{"timeout", "agent.yaml"},
			yamlSrc: `command: "[bad]"
model: haiku
scope: agnostic
timeout: 0
operations:
  run:
    id: run-op
    description: Run.
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
		{
			name: "default_operation_not_defined",
			want: []string{"default_operation", "agent.yaml"},
			yamlSrc: `command: "[bad]"
model: haiku
scope: agnostic
default_operation: ghost
operations:
  run:
    id: run-op
    description: Run.
    output_schema:
      success: {}
      error:
        reason: string
        recoverable: boolean
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAgent(t, dir, "bad", tt.yamlSrc, true)
			_, errs := Load(dir)
			if !hasErrorContaining(errs, tt.want...) {
				t.Errorf("Load errors = %v, want one containing %v", errs, tt.want)
			}
		})
	}
}
