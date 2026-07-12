package envelope

import "fmt"

// Validate runs the five structural checks of agent-protocol.md §2 in order
// and returns the first Violation found, or nil when the envelope is valid.
// wantID is the --id of the originating dispatch (§1.1: id must match it).
func Validate(e *Envelope, wantID string) *Violation {
	if e == nil {
		return &Violation{Check: 1, Msg: "envelope is not a map (nil)"}
	}
	if e.ID == "" {
		return &Violation{Check: 2, Msg: "id is absent or not a string"}
	}
	if e.ID != wantID {
		return &Violation{Check: 2, Msg: fmt.Sprintf("id %q does not match dispatch id %q", e.ID, wantID)}
	}
	switch e.Status {
	case StatusSuccess, StatusError, StatusInfo:
	default:
		return &Violation{Check: 3, Msg: fmt.Sprintf("status %q is absent or not one of {success, error, info}", e.Status)}
	}
	if e.Payload == nil {
		return &Violation{Check: 4, Msg: "payload is absent"}
	}
	if e.Status == StatusError {
		return validateErrorPayload(e.Payload)
	}
	if e.Status == StatusInfo {
		if _, ok := e.Payload.(map[string]any); !ok {
			return &Violation{Check: 4, Msg: "status is info but payload is not an object"}
		}
	}
	return nil
}

// validateErrorPayload enforces §2 check 5: an error envelope must carry an
// object payload with reason (string) and recoverable (boolean).
func validateErrorPayload(payload any) *Violation {
	m, ok := payload.(map[string]any)
	if !ok {
		return &Violation{Check: 5, Msg: "status is error but payload is not an object"}
	}
	if _, ok := m["reason"].(string); !ok {
		return &Violation{Check: 5, Msg: "status is error but payload.reason (string) is missing"}
	}
	if _, ok := m["recoverable"].(bool); !ok {
		return &Violation{Check: 5, Msg: "status is error but payload.recoverable (boolean) is missing"}
	}
	return nil
}
