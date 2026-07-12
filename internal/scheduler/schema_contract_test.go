package scheduler

import (
	"strings"
	"testing"

	"arkannie/internal/envelope"
)

// badSuccess is a success envelope whose out field has the wrong type,
// violating the fixture schema success: {out: string}.
func badSuccess(id string) string {
	return "id: " + id + "\nstatus: success\npayload:\n  out: 42\n"
}

// TestValidateSchema covers U4 (R8): success/info payloads are matched against
// the operation's declared schema; error payloads are not.
func TestValidateSchema(t *testing.T) {
	s := newTestScheduler(t, newStub(), "worker")
	a, ok := s.Reg.Resolve("worker")
	if !ok {
		t.Fatal("worker agent not registered")
	}
	prep := &preparedDispatch{a: a, opName: "run", did: "x"}

	tests := []struct {
		name   string
		env    *envelope.Envelope
		wantOK bool
	}{
		{
			name:   "success_match",
			env:    &envelope.Envelope{ID: "x", Status: envelope.StatusSuccess, Payload: map[string]any{"out": "ok"}},
			wantOK: true,
		},
		{
			name:   "success_mismatch_type",
			env:    &envelope.Envelope{ID: "x", Status: envelope.StatusSuccess, Payload: map[string]any{"out": 42}},
			wantOK: false,
		},
		{
			name:   "success_mismatch_missing",
			env:    &envelope.Envelope{ID: "x", Status: envelope.StatusSuccess, Payload: map[string]any{}},
			wantOK: false,
		},
		{
			name:   "error_not_checked",
			env:    &envelope.Envelope{ID: "x", Status: envelope.StatusError, Payload: map[string]any{"reason": "boom", "recoverable": true}},
			wantOK: true,
		},
		{
			name:   "info_mismatch",
			env:    &envelope.Envelope{ID: "x", Status: envelope.StatusInfo, Payload: map[string]any{"wrong": "x"}},
			wantOK: false,
		},
		{
			// Strict success: a field beyond the declared schema is a contract
			// violation (catches silent field drift between heterogeneous agents).
			name:   "success_unknown_field",
			env:    &envelope.Envelope{ID: "x", Status: envelope.StatusSuccess, Payload: map[string]any{"out": "ok", "extra": "x"}},
			wantOK: false,
		},
		{
			// Lax info: extra fields are allowed so the Ask Protocol may add
			// missing_field/resumable beyond the declared message. (success-only rigor.)
			name:   "info_extra_field_ok",
			env:    &envelope.Envelope{ID: "x", Status: envelope.StatusInfo, Payload: map[string]any{"message": "need type", "missing_field": "type"}},
			wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := s.validateSchema(prep, tt.env)
			if tt.wantOK {
				if v != nil {
					t.Errorf("validateSchema() = %+v, want nil", v)
				}
				return
			}
			if v == nil {
				t.Fatal("validateSchema() = nil, want a schema Violation")
			}
			if v.Kind != envelope.ViolationSchema {
				t.Errorf("Violation.Kind = %d, want ViolationSchema", v.Kind)
			}
		})
	}
}

// TestSchemaMismatchRouting covers U5 (R9): a mismatch surviving the retry
// becomes a catchable error envelope, routed to error -> {} when present and
// to escUnhandledError (Class B, not malformed) otherwise; a retry that fixes
// the payload recovers.
func TestSchemaMismatchRouting(t *testing.T) {
	t.Run("caught_by_error_handler", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{badSuccess("a"), badSuccess("a")}
		s := newTestScheduler(t, stub, "worker")
		prog := parseProg(t, "[worker] --id=a : \"x\"\n  error -> {\n    [notify] : \"caught\"\n  }\n")
		res := s.Run(prog, "sc1", "")
		if res.Esc != nil {
			t.Fatalf("mismatch should be caught, got escalation: %s", res.Esc.Format())
		}
		if countContaining(s.Notices, "caught") != 1 {
			t.Errorf("error handler did not run; notices=%v", s.Notices)
		}
		if stub.calls["a"] != 2 {
			t.Errorf("attempts = %d, want 2 (one corrective retry)", stub.calls["a"])
		}
	})

	t.Run("unhandled_is_error_not_malformed", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{badSuccess("a"), badSuccess("a")}
		s := newTestScheduler(t, stub, "worker")
		res := s.Run(parseProg(t, "[worker] --id=a : \"x\"\n"), "sc2", "")
		if res.Esc == nil || res.Esc.Class != 'B' {
			t.Fatalf("want Class B escalation, got %+v", res.Esc)
		}
		if res.Esc.Title != "unhandled wave error" {
			t.Errorf("title = %q, want unhandled wave error (schema mismatch routes as error, not malformed)", res.Esc.Title)
		}
	})

	t.Run("parallel_mismatch_routes_to_each", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{badSuccess("a"), badSuccess("a")}
		s := newTestScheduler(t, stub, "worker")
		prog := parseProg(t, "parallel {\n  [worker] --id=a : \"1\"\n  [worker] --id=b : \"2\"\n}\n  each -> {\n    [notify] : \"each-ran\"\n  }\n")
		res := s.Run(prog, "sc4", "")
		if res.Esc != nil {
			t.Fatalf("each handler must absorb the mismatch, got: %s", res.Esc.Format())
		}
		if got := countContaining(s.Notices, "each-ran"); got != 2 {
			t.Errorf("each handler ran %d times, want 2", got)
		}
	})

	t.Run("retry_recovers", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{badSuccess("a"), okEnv("a", "out", "fixed")}
		s := newTestScheduler(t, stub, "worker")
		res := s.Run(parseProg(t, "$x = [worker] --id=a : \"s\"\n[worker] --id=b : \"$x\"\n"), "sc3", "")
		if res.Esc != nil {
			t.Fatalf("retry should have recovered, got: %s", res.Esc.Format())
		}
		if stub.calls["a"] != 2 {
			t.Errorf("attempts = %d, want 2", stub.calls["a"])
		}
		if second := stub.prompts["b"]; len(second) == 0 || !strings.Contains(second[0], "out: fixed") {
			t.Errorf("recovered payload not bound into second dispatch: %v", stub.prompts["b"])
		}
	})
}
