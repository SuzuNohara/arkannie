package envelope

import (
	"reflect"
	"testing"
)

func TestTimeout(t *testing.T) {
	t.Run("U10-T6_synthesizes_section_4_3_envelope", func(t *testing.T) {
		got := Timeout("seek", 30)
		want := &Envelope{
			ID:     "seek",
			Status: StatusError,
			Payload: map[string]any{
				"reason":      "Dispatch timed out after 30s",
				"recoverable": true,
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Timeout() = %+v, want %+v", got, want)
		}
		if v := Validate(got, "seek"); v != nil {
			t.Errorf("Validate(Timeout()) = &Violation{Check: %d, Msg: %q}, want nil", v.Check, v.Msg)
		}
	})
}
