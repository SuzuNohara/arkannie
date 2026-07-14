package ann

import "testing"

// lexOne tokenizes a single line and fails on any lexical error.
func lexOne(t *testing.T, src string) []token {
	t.Helper()
	toks, err := lexLine(src, 1)
	if err != nil {
		t.Fatalf("lexLine(%q) unexpected error: %v", src, err)
	}
	return toks
}

func lastToken(t *testing.T, src string) token {
	t.Helper()
	toks := lexOne(t, src)
	if len(toks) == 0 {
		t.Fatalf("lexLine(%q) produced no tokens", src)
	}
	return toks[len(toks)-1]
}

// TestLexStringEscapes covers the v0.3 quoting rules for string literals
// (§ quoting, deferred from v0.2): \" and \\ are real escapes, \$ is kept
// verbatim for the interpolation escape pass, any other \X is a lexical error.
func TestLexStringEscapes(t *testing.T) {
	t.Run("U1-T3_escaped_quote_and_backslash", func(t *testing.T) {
		last := lastToken(t, `$x = "con \"comilla\" y \\backslash"`)
		if last.kind != tkString {
			t.Fatalf("last token kind = %v, want tkString", last.kind)
		}
		want := `con "comilla" y \backslash`
		if last.text != want {
			t.Fatalf("string content = %q, want %q", last.text, want)
		}
	})

	t.Run("U1-T3_invalid_escape_is_lexical_error_LC", func(t *testing.T) {
		// `$x = "bad \q here"` — the offending backslash is at index 10 (col 11).
		_, err := lexLine(`$x = "bad \q here"`, 7)
		if err == nil {
			t.Fatal("expected a lexical error for invalid escape \\q")
		}
		if err.Category != Syntax {
			t.Fatalf("category = %v, want Syntax", err.Category)
		}
		if err.Line != 7 || err.Col != 11 {
			t.Fatalf("error at %d:%d, want 7:11 (the backslash position)", err.Line, err.Col)
		}
	})

	t.Run("U1-T3_escaped_dollar_kept_verbatim", func(t *testing.T) {
		last := lastToken(t, `$x = "price \$5"`)
		if last.text != `price \$5` {
			t.Fatalf("string content = %q, want %q (\\$ preserved for the escape pass)", last.text, `price \$5`)
		}
	})

	t.Run("U1-T8_regression_templates_refs_comments_verbatim", func(t *testing.T) {
		last := lastToken(t, `$x = "{{ slot }} and $ref stay // verbatim"`)
		want := "{{ slot }} and $ref stay // verbatim"
		if last.text != want {
			t.Fatalf("string content = %q, want %q", last.text, want)
		}
	})

	t.Run("U1-T3_unterminated_string_error", func(t *testing.T) {
		if _, err := lexLine(`$x = "open`, 1); err == nil {
			t.Fatal("expected an unterminated string literal error")
		}
	})
}

// TestCollectContextMultiline covers the v0.3 multi-line context semantics:
// an internal blank line is preserved (the central RED), the block ends at a
// dedent, a '}' line or a line containing '->', and internal indentation is
// preserved relative to the block's first line (only the common prefix is cut).
func TestCollectContextMultiline(t *testing.T) {
	t.Run("U1-T5_internal_blank_line_preserved", func(t *testing.T) {
		lines := []string{"  first", "", "  third", "[next]"}
		got, next := collectContext(lines, 0)
		if want := "first\n\nthird"; got != want {
			t.Fatalf("context = %q, want %q (internal blank must be kept)", got, want)
		}
		if next != 3 {
			t.Fatalf("next = %d, want 3 (stops at the dedented line)", next)
		}
	})

	t.Run("U1-T6_dedent_terminates", func(t *testing.T) {
		got, next := collectContext([]string{"    body line", "not indented"}, 0)
		if got != "body line" || next != 1 {
			t.Fatalf("got (%q,%d), want (%q,1)", got, next, "body line")
		}
	})

	t.Run("U1-T6_close_brace_terminates", func(t *testing.T) {
		got, next := collectContext([]string{"    body", "}"}, 0)
		if got != "body" || next != 1 {
			t.Fatalf("got (%q,%d), want (%q,1)", got, next, "body")
		}
	})

	t.Run("U1-T6_arrow_terminates", func(t *testing.T) {
		// indent equals the base indent so only the '->' rule can terminate.
		got, next := collectContext([]string{"  body", "  success -> {"}, 0)
		if got != "body" || next != 1 {
			t.Fatalf("got (%q,%d), want (%q,1)", got, next, "body")
		}
	})

	t.Run("U1-T7_relative_indentation_preserved", func(t *testing.T) {
		lines := []string{"  parent", "    child", "      grandchild", "[next]"}
		got, _ := collectContext(lines, 0)
		if want := "parent\n  child\n    grandchild"; got != want {
			t.Fatalf("context = %q, want %q (relative indent kept)", got, want)
		}
	})

	t.Run("U1-T7_golden_blank_and_indent_together", func(t *testing.T) {
		lines := []string{
			"    intro paragraph",
			"",
			"    - item with detail",
			"        nested note",
			"$after = x",
		}
		got, _ := collectContext(lines, 0)
		if want := "intro paragraph\n\n- item with detail\n    nested note"; got != want {
			t.Fatalf("context = %q, want %q", got, want)
		}
	})

	t.Run("U1-T7_trailing_blank_lines_dropped", func(t *testing.T) {
		lines := []string{"  only", "", "", "[next]"}
		got, _ := collectContext(lines, 0)
		if got != "only" {
			t.Fatalf("context = %q, want %q (trailing blanks are separators)", got, "only")
		}
	})

	t.Run("U1-T8_first_line_not_indented_no_context", func(t *testing.T) {
		got, next := collectContext([]string{"not context", "  x"}, 0)
		if got != "" || next != 0 {
			t.Fatalf("got (%q,%d), want (empty,0)", got, next)
		}
	})
}

// TestEscapedDollarSurvivesParsing confirms \$ is carried verbatim through the
// parser into the arg, string-literal and list-element positions, so the
// interpolation escape pass can later turn it into a literal '$' (U1-T4).
func TestEscapedDollarSurvivesParsing(t *testing.T) {
	t.Run("U1-T4_string_literal_keeps_escape", func(t *testing.T) {
		prog := mustParse(t, "$s = \"\\$lit\"\n", PromptMode)
		lit := prog.Statements[0].(*Assign).Expr.(StrLit)
		if lit.Value != `\$lit` {
			t.Fatalf("string literal = %q, want %q", lit.Value, `\$lit`)
		}
	})

	t.Run("U1-T4_list_element_keeps_escape", func(t *testing.T) {
		prog := mustParse(t, "$l = list(\"\\$a\", \"b\")\n", PromptMode)
		lst := prog.Statements[0].(*Assign).Expr.(ListLit)
		if got := elemSrc(lst.Elems[0]); got != `\$a` {
			t.Fatalf("list element = %q, want %q", got, `\$a`)
		}
	})

	t.Run("U1-T4_dispatch_arg_keeps_escape", func(t *testing.T) {
		prog := mustParse(t, "[cmd] \"\\$arg\"\n", PromptMode)
		d := prog.Statements[0].(*Dispatch)
		if len(d.Args) == 0 || d.Args[0] != `\$arg` {
			t.Fatalf("args = %q, want first %q", d.Args, `\$arg`)
		}
	})
}
