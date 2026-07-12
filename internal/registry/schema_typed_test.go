package registry

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// nodeFrom unmarshals src as a YAML value node (unwrapping the document).
func nodeFrom(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal %q: %v", src, err)
	}
	if len(doc.Content) == 0 {
		return nil
	}
	return doc.Content[0]
}

// TestParsePayloadSchema covers U3 (R5): scalar and object forms, plus the
// invalid-scalar error.
func TestParsePayloadSchema(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantKind  SchemaKind
		wantField map[string]string
		wantErr   bool
	}{
		{name: "scalar_string", src: "string", wantKind: KindString},
		{name: "scalar_list", src: "list", wantKind: KindList},
		{name: "object", src: "echo: string", wantKind: KindObject, wantField: map[string]string{"echo": "string"}},
		{name: "empty_object", src: "{}", wantKind: KindObject, wantField: map[string]string{}},
		{name: "invalid_scalar", src: "number", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := parsePayloadSchema(nodeFrom(t, tt.src))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parsePayloadSchema(%q) err = nil, want error", tt.src)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePayloadSchema(%q) err = %v", tt.src, err)
			}
			if s.Kind != tt.wantKind {
				t.Errorf("Kind = %d, want %d", s.Kind, tt.wantKind)
			}
			for k, want := range tt.wantField {
				if s.Fields[k] != want {
					t.Errorf("Fields[%q] = %q, want %q", k, s.Fields[k], want)
				}
			}
		})
	}
}

// TestMatch covers U3 (R6, R7): lax object matching, kind matching, and that
// the reason never leaks payload values.
func TestMatch(t *testing.T) {
	obj := &PayloadSchema{Kind: KindObject, Fields: map[string]string{"echo": "string"}}
	str := &PayloadSchema{Kind: KindString}
	lst := &PayloadSchema{Kind: KindList}
	empty := &PayloadSchema{Kind: KindObject, Fields: map[string]string{}}

	tests := []struct {
		name    string
		schema  *PayloadSchema
		payload any
		wantOK  bool
	}{
		{name: "object_ok", schema: obj, payload: map[string]any{"echo": "hi"}, wantOK: true},
		{name: "object_unknown_field_rejected", schema: obj, payload: map[string]any{"echo": "hi", "x": 1}, wantOK: false},
		{name: "object_missing_field", schema: obj, payload: map[string]any{}, wantOK: false},
		{name: "object_wrong_type", schema: obj, payload: map[string]any{"echo": 42}, wantOK: false},
		{name: "object_vs_string", schema: obj, payload: "hi", wantOK: false},
		{name: "string_ok", schema: str, payload: "hi", wantOK: true},
		{name: "string_vs_int", schema: str, payload: 42, wantOK: false},
		{name: "list_ok", schema: lst, payload: []any{"a"}, wantOK: true},
		{name: "list_empty_ok", schema: lst, payload: []any{}, wantOK: true},
		{name: "empty_object_any_map", schema: empty, payload: map[string]any{"whatever": 1}, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.schema.Match(tt.payload, true)
			if tt.wantOK && got != "" {
				t.Errorf("Match() = %q, want \"\"", got)
			}
			if !tt.wantOK && got == "" {
				t.Errorf("Match() = \"\", want a reason")
			}
		})
	}

	// SEC1: the reason must not contain the offending payload value.
	t.Run("reason_hides_value", func(t *testing.T) {
		got := str.Match(42, true)
		if got == "" {
			t.Fatal("expected a mismatch reason")
		}
		if contains(got, "42") {
			t.Errorf("reason %q leaks the payload value", got)
		}
		if !contains(got, "string") || !contains(got, "integer") {
			t.Errorf("reason %q should name expected and received types", got)
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestBuildAgentSchemas covers U3 (R5): loading a contract populates the
// parsed SuccessSchema and InfoSchema on each operation.
func TestBuildAgentSchemas(t *testing.T) {
	src := []byte(`command: "[echo]"
model: haiku
scope: agnostic
operations:
  echo:
    id: echo-op
    description: Echo.
    output_schema:
      success:
        echo: string
      error:
        reason: string
        recoverable: boolean
      info:
        message: string
`)
	af, err := parseAgentFile(src)
	if err != nil {
		t.Fatalf("parseAgentFile: %v", err)
	}
	a := buildAgent("/tmp/echo", src, af, "harness")
	op := a.Operations["echo"]
	if op.SuccessSchema == nil || op.SuccessSchema.Kind != KindObject {
		t.Fatalf("SuccessSchema = %+v, want KindObject", op.SuccessSchema)
	}
	if op.SuccessSchema.Fields["echo"] != "string" {
		t.Errorf("SuccessSchema.Fields[echo] = %q, want string", op.SuccessSchema.Fields["echo"])
	}
	if op.InfoSchema == nil || op.InfoSchema.Fields["message"] != "string" {
		t.Errorf("InfoSchema = %+v, want message:string", op.InfoSchema)
	}
}
