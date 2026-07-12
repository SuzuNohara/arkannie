package envelope

import (
	"reflect"
	"testing"
)

// TestExtractTypedPayload covers U1: a success payload may be a bare string,
// a list, or be absent — Extract no longer forces a map.
func TestExtractTypedPayload(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   any
	}{
		{
			name:   "string_payload",
			result: "id: seek\nstatus: success\npayload: hola\n",
			want:   "hola",
		},
		{
			name:   "list_payload",
			result: "id: seek\nstatus: success\npayload:\n  - a\n  - b\n",
			want:   []any{"a", "b"},
		},
		{
			name:   "object_payload",
			result: "id: seek\nstatus: success\npayload:\n  echo: hi\n",
			want:   map[string]any{"echo": "hi"},
		},
		{
			name:   "absent_payload",
			result: "id: seek\nstatus: success\n",
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Extract(claudeJSON(t, tt.result))
			if err != nil {
				t.Fatalf("Extract() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(e.Payload, tt.want) {
				t.Errorf("Payload = %#v, want %#v", e.Payload, tt.want)
			}
		})
	}
}

// TestValidateTypedPayload covers U2: check 4 accepts non-map success
// payloads; error and info payloads must be objects.
func TestValidateTypedPayload(t *testing.T) {
	tests := []struct {
		name      string
		e         *Envelope
		wantNil   bool
		wantCheck int
	}{
		{
			name:    "success_string_payload_passes",
			e:       &Envelope{ID: "seek", Status: StatusSuccess, Payload: "hola"},
			wantNil: true,
		},
		{
			name:    "success_list_payload_passes",
			e:       &Envelope{ID: "seek", Status: StatusSuccess, Payload: []any{"a"}},
			wantNil: true,
		},
		{
			name:      "error_non_object_fails_check5",
			e:         &Envelope{ID: "seek", Status: StatusError, Payload: "boom"},
			wantCheck: 5,
		},
		{
			name:      "info_non_object_fails_check4",
			e:         &Envelope{ID: "seek", Status: StatusInfo, Payload: "note"},
			wantCheck: 4,
		},
		{
			name:    "info_object_passes",
			e:       &Envelope{ID: "seek", Status: StatusInfo, Payload: map[string]any{"message": "hi"}},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Validate(tt.e, "seek")
			if tt.wantNil {
				if v != nil {
					t.Errorf("Validate() = %+v, want nil", v)
				}
				return
			}
			if v == nil || v.Check != tt.wantCheck {
				t.Errorf("Validate() = %+v, want Violation with Check %d", v, tt.wantCheck)
			}
		})
	}
}

// TestSchemaError covers U5: the synthesized catchable error envelope carries
// reason, recoverable=true and kind=malformed_envelope.
func TestSchemaError(t *testing.T) {
	e := SchemaError("seek", `field "echo" expected string, got integer`)
	if e.ID != "seek" {
		t.Errorf("ID = %q, want seek", e.ID)
	}
	if e.Status != StatusError {
		t.Errorf("Status = %q, want error", e.Status)
	}
	p, ok := e.Payload.(map[string]any)
	if !ok {
		t.Fatalf("Payload is %T, want map[string]any", e.Payload)
	}
	if p["reason"] != `field "echo" expected string, got integer` {
		t.Errorf("reason = %v", p["reason"])
	}
	if p["recoverable"] != true {
		t.Errorf("recoverable = %v, want true", p["recoverable"])
	}
	if p["kind"] != "malformed_envelope" {
		t.Errorf("kind = %v, want malformed_envelope", p["kind"])
	}
	// The synthesized envelope must itself pass structural validation so it
	// routes as a normal error.
	if v := Validate(e, "seek"); v != nil {
		t.Errorf("Validate() = %+v, want nil", v)
	}
}
