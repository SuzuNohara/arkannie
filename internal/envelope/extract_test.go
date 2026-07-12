package envelope

import (
	"encoding/json"
	"reflect"
	"testing"
)

// claudeJSON wraps an agent result string in the outer JSON shape produced
// by `claude -p --output-format json`.
func claudeJSON(t *testing.T, result string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":     "result",
		"is_error": false,
		"result":   result,
	})
	if err != nil {
		t.Fatalf("marshaling claude JSON fixture: %v", err)
	}
	return b
}

func TestExtract(t *testing.T) {
	t.Run("U10-T1_clean_yaml_envelope", func(t *testing.T) {
		result := "id: seek\nstatus: success\npayload:\n  found: true\n  count: 3\n"
		e, err := Extract(claudeJSON(t, result))
		if err != nil {
			t.Fatalf("Extract() error = %v, want nil", err)
		}
		want := &Envelope{
			ID:     "seek",
			Status: StatusSuccess,
			Payload: map[string]any{
				"found": true,
				"count": 3,
			},
			Raw: result,
		}
		if !reflect.DeepEqual(e, want) {
			t.Errorf("Extract() = %+v, want %+v", e, want)
		}
	})

	t.Run("U10-T2_yaml_fence", func(t *testing.T) {
		result := "```yaml\nid: seek\nstatus: info\npayload:\n  message: working\n```"
		e, err := Extract(claudeJSON(t, result))
		if err != nil {
			t.Fatalf("Extract() error = %v, want nil", err)
		}
		want := &Envelope{
			ID:      "seek",
			Status:  StatusInfo,
			Payload: map[string]any{"message": "working"},
			Raw:     result,
		}
		if !reflect.DeepEqual(e, want) {
			t.Errorf("Extract() = %+v, want %+v", e, want)
		}
	})

	t.Run("U10-T3_surrounding_text", func(t *testing.T) {
		t.Run("fence_with_text_around_extracted", func(t *testing.T) {
			result := "Here is the envelope you asked for:\n\n" +
				"```\nid: seek\nstatus: success\npayload: {}\n```\n\nDone!"
			e, err := Extract(claudeJSON(t, result))
			if err != nil {
				t.Fatalf("Extract() error = %v, want nil", err)
			}
			want := &Envelope{
				ID:      "seek",
				Status:  StatusSuccess,
				Payload: map[string]any{},
				Raw:     result,
			}
			if !reflect.DeepEqual(e, want) {
				t.Errorf("Extract() = %+v, want %+v", e, want)
			}
		})

		t.Run("no_recoverable_yaml_is_extraction_error", func(t *testing.T) {
			result := "I could not complete the task, sorry about that."
			if _, err := Extract(claudeJSON(t, result)); err == nil {
				t.Fatal("Extract() error = nil, want extraction error")
			}
		})

		t.Run("stdout_not_json_is_extraction_error", func(t *testing.T) {
			if _, err := Extract([]byte("not json at all")); err == nil {
				t.Fatal("Extract() error = nil, want extraction error")
			}
		})

		t.Run("missing_result_field_is_extraction_error", func(t *testing.T) {
			if _, err := Extract([]byte(`{"type":"result"}`)); err == nil {
				t.Fatal("Extract() error = nil, want extraction error")
			}
		})

		t.Run("non_string_result_is_extraction_error", func(t *testing.T) {
			if _, err := Extract([]byte(`{"result":42}`)); err == nil {
				t.Fatal("Extract() error = nil, want extraction error")
			}
		})
	})

	t.Run("U10-T7_nested_payload_types", func(t *testing.T) {
		result := "id: deep\nstatus: success\npayload:\n" +
			"  count: 3\n" +
			"  ratio: 0.5\n" +
			"  ok: true\n" +
			"  items:\n" +
			"    - a\n" +
			"    - 2\n" +
			"    - nested:\n" +
			"        x: 1\n" +
			"  meta:\n" +
			"    tags: [go, yaml]\n" +
			"    depth: 2\n"
		e, err := Extract(claudeJSON(t, result))
		if err != nil {
			t.Fatalf("Extract() error = %v, want nil", err)
		}
		want := map[string]any{
			"count": 3,
			"ratio": 0.5,
			"ok":    true,
			"items": []any{
				"a",
				2,
				map[string]any{"nested": map[string]any{"x": 1}},
			},
			"meta": map[string]any{
				"tags":  []any{"go", "yaml"},
				"depth": 2,
			},
		}
		if !reflect.DeepEqual(e.Payload, want) {
			t.Errorf("Extract() payload = %#v, want %#v", e.Payload, want)
		}
	})
}
