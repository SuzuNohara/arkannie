package envelope

import "testing"

func TestValidate(t *testing.T) {
	t.Run("U10-T4_malformed_fixtures", func(t *testing.T) {
		tests := []struct {
			name      string
			e         *Envelope
			wantID    string
			wantCheck int
		}{
			{
				name:      "check1_not_a_map",
				e:         nil,
				wantID:    "seek",
				wantCheck: 1,
			},
			{
				name:      "check2_id_missing",
				e:         &Envelope{ID: "", Status: StatusSuccess, Payload: map[string]any{}},
				wantID:    "seek",
				wantCheck: 2,
			},
			{
				name:      "check3_invalid_status",
				e:         &Envelope{ID: "seek", Status: Status("done"), Payload: map[string]any{}},
				wantID:    "seek",
				wantCheck: 3,
			},
			{
				name:      "check4_payload_nil",
				e:         &Envelope{ID: "seek", Status: StatusSuccess, Payload: nil},
				wantID:    "seek",
				wantCheck: 4,
			},
			{
				name:      "check5_error_without_reason_recoverable",
				e:         &Envelope{ID: "seek", Status: StatusError, Payload: map[string]any{}},
				wantID:    "seek",
				wantCheck: 5,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				v := Validate(tt.e, tt.wantID)
				if v == nil {
					t.Fatalf("Validate() = nil, want Violation with Check %d", tt.wantCheck)
				}
				if v.Check != tt.wantCheck {
					t.Errorf("Validate() Check = %d (%s), want %d", v.Check, v.Msg, tt.wantCheck)
				}
				if v.Msg == "" {
					t.Error("Validate() Msg is empty, want a description")
				}
			})
		}
	})

	t.Run("U10-T5_id_mismatch", func(t *testing.T) {
		e := &Envelope{ID: "other", Status: StatusSuccess, Payload: map[string]any{}}
		v := Validate(e, "seek")
		if v == nil {
			t.Fatal("Validate() = nil, want Violation with Check 2")
		}
		if v.Check != 2 {
			t.Errorf("Validate() Check = %d (%s), want 2", v.Check, v.Msg)
		}
	})

	t.Run("valid_envelopes_pass", func(t *testing.T) {
		tests := []struct {
			name string
			e    *Envelope
		}{
			{
				name: "success_empty_payload",
				e:    &Envelope{ID: "seek", Status: StatusSuccess, Payload: map[string]any{}},
			},
			{
				name: "info_with_message",
				e:    &Envelope{ID: "seek", Status: StatusInfo, Payload: map[string]any{"message": "hi"}},
			},
			{
				name: "error_with_reason_and_recoverable",
				e: &Envelope{ID: "seek", Status: StatusError, Payload: map[string]any{
					"reason":      "boom",
					"recoverable": false,
				}},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if v := Validate(tt.e, "seek"); v != nil {
					t.Errorf("Validate() = &Violation{Check: %d, Msg: %q}, want nil", v.Check, v.Msg)
				}
			})
		}
	})

	t.Run("error_with_wrong_types_fails_check5", func(t *testing.T) {
		tests := []struct {
			name    string
			payload map[string]any
		}{
			{name: "reason_not_string", payload: map[string]any{"reason": 42, "recoverable": true}},
			{name: "recoverable_not_bool", payload: map[string]any{"reason": "boom", "recoverable": "yes"}},
			{name: "recoverable_missing", payload: map[string]any{"reason": "boom"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				e := &Envelope{ID: "seek", Status: StatusError, Payload: tt.payload}
				v := Validate(e, "seek")
				if v == nil || v.Check != 5 {
					t.Errorf("Validate() = %+v, want Violation with Check 5", v)
				}
			})
		}
	})
}
