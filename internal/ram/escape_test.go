package ram

import "testing"

// TestEscapePlaceholderRoundTrip covers the single-pass \$ escape helper that
// lives beside RefToken: EscapePlaceholder masks every \$ so RefToken skips it
// during interpolation, and RestoreEscapes turns the mask into a literal '$'
// once resolution is done (§ quoting, U1-T3/U1-T4).
func TestEscapePlaceholderRoundTrip(t *testing.T) {
	t.Run("U1-T3_escaped_dollar_hidden_from_reftoken", func(t *testing.T) {
		masked := EscapePlaceholder(`literal \$name here`)
		if got := RefToken.FindString(masked); got != "" {
			t.Fatalf("RefToken matched %q inside masked text; \\$ must be hidden", got)
		}
		if got := RestoreEscapes(masked); got != "literal $name here" {
			t.Fatalf("restored = %q, want %q", got, "literal $name here")
		}
	})

	t.Run("U1-T4_real_ref_survives_masking", func(t *testing.T) {
		masked := EscapePlaceholder(`take $real not \$fake`)
		if got := RefToken.FindString(masked); got != "$real" {
			t.Fatalf("real ref lost after masking: matched %q, want %q", got, "$real")
		}
	})

	t.Run("U1-T4_mixed_positions_roundtrip", func(t *testing.T) {
		in := `arg \$a, "\$s", list(\$l), ctx \$c`
		got := RestoreEscapes(EscapePlaceholder(in))
		if want := `arg $a, "$s", list($l), ctx $c`; got != want {
			t.Fatalf("roundtrip = %q, want %q", got, want)
		}
	})

	t.Run("U1-T4_no_escape_is_identity", func(t *testing.T) {
		in := `plain $ref and text`
		if got := RestoreEscapes(EscapePlaceholder(in)); got != in {
			t.Fatalf("identity broken: %q -> %q", in, got)
		}
	})
}
