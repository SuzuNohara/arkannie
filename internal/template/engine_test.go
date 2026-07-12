package template

import "testing"

func TestRender(t *testing.T) {
	tests := []struct {
		name  string
		tmpl  string
		slots map[string]string
		want  string
	}{
		// U3-T1 — §5.4 null-handling table: {absent, empty} per construct.
		{"U3-T1_slot_absent", "[{{ k }}]", nil, "[]"},
		{"U3-T1_slot_empty", "[{{ k }}]", map[string]string{"k": ""}, "[]"},
		{"U3-T1_if_absent", "{{#if k}}content{{/if}}", nil, ""},
		{"U3-T1_if_empty", "{{#if k}}content{{/if}}", map[string]string{"k": ""}, ""},
		{"U3-T1_fallback_absent", "{{ k | fb }}", nil, "fb"},
		{"U3-T1_fallback_empty", "{{ k | fb }}", map[string]string{"k": ""}, "fb"},

		// Present-value counterparts and spacing tolerance (§5.1–§5.3).
		{"slot_present", "[{{ k }}]", map[string]string{"k": "v"}, "[v]"},
		{"slot_no_inner_spaces", "[{{k}}]", map[string]string{"k": "v"}, "[v]"},
		{"if_present_content_verbatim", "a {{#if k}} mid {{/if}} b", map[string]string{"k": "v"}, "a  mid  b"},
		{"fallback_present", "{{ k | fb }}", map[string]string{"k": "v"}, "v"},
		{"fallback_tight_spaces", "{{ k|fb }}", nil, "fb"},
		{"fallback_no_spaces", "{{k|fb}}", nil, "fb"},
		{"fallback_keeps_inner_pipes", "{{ k | a|b }}", nil, "a|b"},

		// U3-T2 — §5.5 render order: #if blocks first, then simple and
		// fallback slots; remaining unresolved slots render as "".
		{
			"U3-T2_order_if_then_slots_then_unresolved",
			"Hello {{ name }}!\n{{#if extra}}note: {{ extra }}{{/if}}\nend {{ missing }}.",
			map[string]string{"name": "World", "extra": "yes"},
			"Hello World!\nnote: yes\nend .",
		},
		{
			"U3-T2_order_absent_if_and_unresolved",
			"Hello {{ name }}!\n{{#if extra}}note: {{ extra }}{{/if}}\nend {{ missing }}.",
			map[string]string{"name": "World"},
			"Hello World!\nend .",
		},
		{
			"U3-T2_unresolved_slot_inside_kept_if",
			"{{#if a}}[{{ nope }}]{{/if}}",
			map[string]string{"a": "1"},
			"[]",
		},

		// U3-T3 — §5.3 fallback text is literal, never expanded.
		{"U3-T3_fallback_literal_not_expanded", "{{ a | {{ b }} }}", map[string]string{"b": "BEE"}, "{{ b }}"},
		{"U3-T3_fallback_unused_when_present", "{{ a | {{ b }} }}", map[string]string{"a": "A", "b": "BEE"}, "A"},

		// U3-T4 — §5.2 absent block removed with surrounding whitespace:
		// no phantom blank lines left behind.
		{
			"U3-T4_standalone_block_leaves_no_blank_line",
			"line1\n{{#if x}}\nmiddle {{ x }}\n{{/if}}\nline2\n",
			nil,
			"line1\nline2\n",
		},
		{
			"U3-T4_indented_block_leaves_no_blank_line",
			"line1\n  {{#if x}}\n  middle\n  {{/if}}\nline2",
			nil,
			"line1\nline2",
		},
		{"U3-T4_inline_block_surrounding_spaces_removed", "hello {{#if x}}world{{/if}}!", nil, "hello!"},
		{"U3-T4_between_words", "A {{#if k}}X{{/if}} B", nil, "AB"},
		{
			"U3-T4_two_absent_blocks",
			"a\n{{#if x}}\nX\n{{/if}}\n{{#if y}}\nY\n{{/if}}\nb\n",
			nil,
			"a\nb\n",
		},

		// U3-T5 — repeated slots on one line and multi-line values.
		{"U3-T5_multiple_slots_same_line", "{{ a }}-{{b}}-{{ a }}", map[string]string{"a": "1", "b": "2"}, "1-2-1"},
		{"U3-T5_value_with_newlines", "start {{ v }} end", map[string]string{"v": "l1\nl2"}, "start l1\nl2 end"},
		{
			"U3-T5_multiple_slots_with_newline_values",
			"{{ a }},{{ b }}!",
			map[string]string{"a": "x\ny", "b": "z\n"},
			"x\ny,z\n!",
		},

		// Robustness edges.
		{"value_never_rescanned", "{{ a }}", map[string]string{"a": "{{ b }}", "b": "X"}, "{{ b }}"},
		{"unclosed_slot_left_verbatim", "text {{ a", nil, "text {{ a"},
		{"unclosed_if_open_tag_left_verbatim", "{{#if x content", nil, "{{#if x content"},
		{"if_missing_close_tag_stripped_as_slot", "{{#if x}}content", map[string]string{"x": "1"}, "content"},
		{"empty_template", "", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Render(tt.tmpl, tt.slots); got != tt.want {
				t.Errorf("Render(%q, %v) = %q, want %q", tt.tmpl, tt.slots, got, tt.want)
			}
		})
	}
}
