package registry

import (
	"fmt"
	"testing"
)

// capYAML is a complete, valid agent contract carrying a full capabilities
// card. Individual tests trim or blank fields to exercise VAL-18.
const capYAML = `command: "[card]"
model: haiku
scope: agnostic
capabilities:
  purpose: Turn a raw requirement into a structured brief.
  use_when: A business problem needs to be scoped before design.
  inputs: free-text requirement
  produces: structured brief
  examples:
    - '[card] : reduce checkout abandonment'
operations:
  run:
    id: run-op
    description: Produce the brief.
    grants: [read]
    output_schema:
      success:
        out: string
      error:
        reason: string
        recoverable: boolean
`

func TestCapabilitiesParse(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "card", capYAML, true)
	reg, errs := Load(dir)
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	a, ok := reg.Resolve("card")
	if !ok {
		t.Fatal("agent card did not load")
	}
	if a.Capabilities == nil {
		t.Fatal("Capabilities is nil, want populated")
	}
	if a.Capabilities.Purpose == "" || a.Capabilities.UseWhen == "" {
		t.Errorf("purpose/use_when empty: %+v", a.Capabilities)
	}
	if a.Capabilities.Inputs != "free-text requirement" || a.Capabilities.Produces != "structured brief" {
		t.Errorf("inputs/produces = %q/%q", a.Capabilities.Inputs, a.Capabilities.Produces)
	}
	if len(a.Capabilities.Examples) != 1 {
		t.Errorf("examples = %v, want 1", a.Capabilities.Examples)
	}
}

func TestCapabilitiesVAL18(t *testing.T) {
	base := `command: "[card]"
model: haiku
scope: agnostic
%s
operations:
  run:
    id: run-op
    description: Produce the brief.
    grants: [read]
    output_schema:
      success:
        out: string
      error:
        reason: string
        recoverable: boolean
`
	tests := []struct {
		name string
		cap  string
	}{
		{"missing_block", ""},
		{"empty_purpose", "capabilities:\n  purpose: \"\"\n  use_when: When scoping.\n"},
		{"empty_use_when", "capabilities:\n  purpose: Scope a requirement.\n  use_when: \"\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := fmt.Sprintf(base, tt.cap)
			writeAgent(t, dir, "card", src, true)
			_, errs := Load(dir)
			if !hasErrorContaining(errs, "VAL-18", "agent.yaml") {
				t.Errorf("Load errors = %v, want one naming VAL-18 and the file", errs)
			}
		})
	}
}
