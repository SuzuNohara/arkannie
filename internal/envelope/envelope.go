// Package envelope implements extraction, validation, and synthesis of the
// trinary agent envelope {id, status, payload} defined by
// spec/agent-protocol.md §1, §2 and §4.3. Corrective retries live in the
// scheduler, not here.
package envelope

import "fmt"

// Status is the trinary envelope status (agent-protocol.md §1.1).
type Status string

// The three literal statuses an envelope may carry.
const (
	StatusSuccess Status = "success"
	StatusError   Status = "error"
	StatusInfo    Status = "info"
)

// Envelope is the structured return of a wave agent (agent-protocol.md §1).
// Payload is untyped: a success payload may be a string, a list ([]any) or an
// object (map[string]any) per the agent's output_schema; error and info
// payloads are always objects.
type Envelope struct {
	ID      string
	Status  Status
	Payload any
	Raw     string // verbatim result text the envelope was extracted from
}

// ViolationKind distinguishes a structural protocol violation from a
// schema-contract mismatch. They share the corrective retry but diverge on
// exhaustion: structural violations escalate (Class B), schema mismatches
// surface as a catchable error envelope (decision D).
type ViolationKind int

// The two violation origins.
const (
	ViolationStructural ViolationKind = iota
	ViolationSchema
)

// Violation identifies the first failed validation step per the ordered
// checks of agent-protocol.md §2 (Check is 1..5). Kind defaults to
// ViolationStructural; schema-mismatch violations set ViolationSchema.
type Violation struct {
	Check int
	Msg   string
	Kind  ViolationKind
}

// SchemaError synthesizes the catchable error envelope raised when a success
// or info payload still violates its declared output_schema after the single
// corrective retry (decision D). reason must name fields and types only,
// never payload values (SEC1).
func SchemaError(id, reason string) *Envelope {
	return &Envelope{
		ID:     id,
		Status: StatusError,
		Payload: map[string]any{
			"reason":      reason,
			"recoverable": true,
			"kind":        "malformed_envelope",
		},
	}
}

// Timeout synthesizes the timeout envelope of agent-protocol.md §4.3 for a
// dispatch identified by id that exceeded secs seconds.
func Timeout(id string, secs int) *Envelope {
	return &Envelope{
		ID:     id,
		Status: StatusError,
		Payload: map[string]any{
			"reason":      fmt.Sprintf("Dispatch timed out after %ds", secs),
			"recoverable": true,
		},
	}
}
