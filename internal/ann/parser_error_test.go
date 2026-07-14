package ann

import (
	"strings"
	"testing"
)

func TestVersionMismatch(t *testing.T) {
	t.Run("U2-T10_wrong_version_v01", func(t *testing.T) {
		err := mustFail(t, string(readTestdata(t, "version_mismatch.ann")), ProgramMode, VersionMismatch)
		if err.Line != 1 {
			t.Errorf("line = %d, want 1", err.Line)
		}
		if err.Class() != 'B' {
			t.Errorf("class = %c, want B", err.Class())
		}
	})
	t.Run("U2-T10_wrong_version_v02", func(t *testing.T) {
		// v0.3 must reject the immediately-previous version too, not only v0.1.
		err := mustFail(t, "# ann v0.2\n[seeker] auth\n", ProgramMode, VersionMismatch)
		if err.Line != 1 {
			t.Errorf("line = %d, want 1", err.Line)
		}
		if err.Class() != 'B' {
			t.Errorf("class = %c, want B", err.Class())
		}
	})
	t.Run("U2-T10_header_not_col0", func(t *testing.T) {
		mustFail(t, " # ann v0.3\n[seeker] auth\n", ProgramMode, VersionMismatch)
	})
	t.Run("U2-T10_header_absent", func(t *testing.T) {
		mustFail(t, "[seeker] auth\n", ProgramMode, VersionMismatch)
	})
	t.Run("U2-T10_empty_source", func(t *testing.T) {
		mustFail(t, "", ProgramMode, VersionMismatch)
	})
}

func TestReturnRules(t *testing.T) {
	t.Run("multiple_returns_require_id", func(t *testing.T) {
		err := mustFail(t, "[return] --id=a \"x\"\n[return] \"y\"\n", PromptMode, Syntax)
		if err.Line != 2 {
			t.Errorf("line = %d, want 2", err.Line)
		}
		if !strings.Contains(err.Msg, "--id") || !strings.Contains(err.Msg, "multiple") {
			t.Errorf("msg = %q, want multiple-returns --id mention", err.Msg)
		}
	})
	t.Run("return_in_loop_requires_id", func(t *testing.T) {
		src := "$i = list(\"a\")\nforeach $i {\n  [return] $item\n}\n"
		err := mustFail(t, src, PromptMode, Syntax)
		if err.Line != 3 {
			t.Errorf("line = %d, want 3", err.Line)
		}
		if !strings.Contains(err.Msg, "loop") || !strings.Contains(err.Msg, "--id") {
			t.Errorf("msg = %q, want loop --id mention", err.Msg)
		}
	})
	t.Run("duplicate_return_id", func(t *testing.T) {
		err := mustFail(t, "[return] --id=a \"x\"\n[return] --id=a \"y\"\n", PromptMode, Syntax)
		if !strings.Contains(err.Msg, "duplicate [return] --id") {
			t.Errorf("msg = %q, want duplicate mention", err.Msg)
		}
	})
	t.Run("single_unlabeled_return_is_valid", func(t *testing.T) {
		if _, err := Parse([]byte("[return] \"x\"\n"), PromptMode); err != nil {
			t.Errorf("single unlabeled return should parse: %v", err)
		}
	})
	t.Run("multiple_unique_ids_are_valid", func(t *testing.T) {
		if _, err := Parse([]byte("[return] --id=a \"x\"\n[return] --id=b \"y\"\n"), PromptMode); err != nil {
			t.Errorf("distinct-id returns should parse: %v", err)
		}
	})
}

func TestUnsupportedConditionals(t *testing.T) {
	for name, src := range map[string]string{
		"U2-T11_bare_while":      "while $x {\n}\n",
		"U2-T11_bracketed_if":    "[if] $x\n",
		"U2-T11_bracketed_while": "[while] $x\n",
	} {
		t.Run(name, func(t *testing.T) {
			err := mustFail(t, src, PromptMode, Syntax)
			if !strings.Contains(err.Msg, "use trinary handlers") {
				t.Errorf("msg = %q, want it to contain \"use trinary handlers\"", err.Msg)
			}
		})
	}
}

func TestIfErrors(t *testing.T) {
	cases := map[string]string{
		"missing_operator":   "if $x {\n}\n",
		"bad_left_operand":   "if foo == \"x\" {\n}\n",
		"bad_right_operand":  "if $x == bar {\n}\n",
		"comma_operand":      "if , == \"x\" {\n}\n",
		"bad_ref_path":       "if $x foo == \"y\" {\n}\n",
		"extra_after_string": "if \"a\" \"b\" == \"c\" {\n}\n",
		"missing_left":       "if == \"x\" {\n}\n",
		"missing_right":      "if $x == {\n}\n",
		"no_open_brace":      "if $x == \"ok\"\n",
		"else_without_brace": "if $x == \"ok\" {\n}\nelse\n",
		"else_extra_tokens":  "if $x == \"ok\" {\n}\nelse foo {\n}\n",
		"standalone_else":    "else {\n}\n",
		"unclosed_then":      "if $x == \"ok\" {\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			err := mustFail(t, src, PromptMode, Syntax)
			if err.Line < 1 || err.Col < 1 {
				t.Errorf("position = %d:%d, want 1-based line:col", err.Line, err.Col)
			}
		})
	}
}

func TestLoopUntilErrors(t *testing.T) {
	cases := map[string]string{
		"missing_guard":       "loop limit=2 until {\n}\n",
		"guard_no_operator":   "loop limit=2 until $x {\n}\n",
		"bad_left_operand":    "loop limit=2 until foo == \"x\" {\n}\n",
		"bad_right_operand":   "loop limit=2 until $x == bar {\n}\n",
		"no_open_brace":       "loop limit=2 until $x == \"ok\"\n",
		"junk_before_brace":   "loop limit=2 foo $x == \"y\" {\n}\n",
		"standalone_until":    "until $x == \"y\" {\n}\n",
		"until_missing_right": "loop limit=2 until $x == {\n}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			err := mustFail(t, src, PromptMode, Syntax)
			if err.Line < 1 || err.Col < 1 {
				t.Errorf("position = %d:%d, want 1-based line:col", err.Line, err.Col)
			}
		})
	}
}

func TestParallelIDErrors(t *testing.T) {
	t.Run("U2-T12_missing_id", func(t *testing.T) {
		src := "parallel {\n  [seeker] --id=a one\n  [reviewer] two\n}\n"
		err := mustFail(t, src, PromptMode, Syntax)
		if err.Line != 3 {
			t.Errorf("line = %d, want 3", err.Line)
		}
		if !strings.Contains(err.Msg, "--id") {
			t.Errorf("msg = %q, want --id mention", err.Msg)
		}
	})
	t.Run("U2-T12_duplicate_id", func(t *testing.T) {
		err := mustFail(t, string(readTestdata(t, "parallel_dup_id.ann")), ProgramMode, Syntax)
		if err.Line != 4 {
			t.Errorf("line = %d, want 4", err.Line)
		}
		if !strings.Contains(err.Msg, "duplicate --id") {
			t.Errorf("msg = %q, want duplicate --id mention", err.Msg)
		}
	})
	t.Run("U2-T12_nested_parallel", func(t *testing.T) {
		src := "parallel {\n  parallel {\n  }\n}\n"
		err := mustFail(t, src, PromptMode, Syntax)
		if !strings.Contains(err.Msg, "nested parallel") {
			t.Errorf("msg = %q, want nested parallel mention", err.Msg)
		}
	})
}

func TestValidateCommands(t *testing.T) {
	known := func(name string) bool { return name == "seeker" || name == "reviewer" }

	t.Run("U2-T13_unknown_command_with_line", func(t *testing.T) {
		prog := mustParse(t, "[seeker] ok\n[ghost] boo\n", PromptMode)
		err := prog.ValidateCommands(known)
		if err == nil {
			t.Fatal("ValidateCommands = nil, want UnknownCommand error")
		}
		if err.Category != UnknownCommand {
			t.Errorf("category = %v, want UnknownCommand", err.Category)
		}
		if err.Line != 2 {
			t.Errorf("line = %d, want 2", err.Line)
		}
		if err.Class() != 'B' {
			t.Errorf("class = %c, want B", err.Class())
		}
	})
	t.Run("U2-T13_builtins_always_known", func(t *testing.T) {
		prog := mustParse(t, "[ask-user] q\n[notify] m\n[clarify] c\n", PromptMode)
		if err := prog.ValidateCommands(func(string) bool { return false }); err != nil {
			t.Errorf("builtins rejected: %v", err)
		}
	})
	t.Run("U2-T13_unknown_nested", func(t *testing.T) {
		src := "$l = list(\"a\")\n" +
			"foreach $l {\n" +
			"  loop limit=2 {\n" +
			"    [seeker] x\n" +
			"      success -> {\n" +
			"        [ghost] hidden\n" +
			"      }\n" +
			"  }\n" +
			"}\n"
		prog := mustParse(t, src, PromptMode)
		err := prog.ValidateCommands(known)
		if err == nil || err.Category != UnknownCommand {
			t.Fatalf("err = %v, want UnknownCommand", err)
		}
		if err.Line != 6 {
			t.Errorf("line = %d, want 6", err.Line)
		}
	})
	t.Run("U2-T13_unknown_in_parallel_and_assign", func(t *testing.T) {
		src := "$r = [ghost] q\n"
		err := mustParse(t, src, PromptMode).ValidateCommands(known)
		if err == nil || err.Category != UnknownCommand || err.Line != 1 {
			t.Fatalf("err = %v, want UnknownCommand at line 1", err)
		}
		src = "parallel {\n  [seeker] --id=a x\n  [ghost] --id=b y\n}\n  each -> {\n    [ghost2] z\n  }\n"
		err = mustParse(t, src, PromptMode).ValidateCommands(known)
		if err == nil || err.Line != 3 {
			t.Fatalf("err = %v, want UnknownCommand at line 3", err)
		}
	})
}

func TestLoopLimitErrors(t *testing.T) {
	t.Run("U2-T14_limit_zero", func(t *testing.T) {
		err := mustFail(t, "loop limit=0 {\n  [seeker] x\n}\n", PromptMode, Type)
		if err.Class() != 'A' {
			t.Errorf("class = %c, want A", err.Class())
		}
	})
	t.Run("U2-T14_limit_negative", func(t *testing.T) {
		err := mustFail(t, "loop limit=-1 {\n  [seeker] x\n}\n", PromptMode, Type)
		if err.Class() != 'A' {
			t.Errorf("class = %c, want A", err.Class())
		}
	})
	t.Run("U2-T14_limit_not_integer", func(t *testing.T) {
		err := mustFail(t, "loop limit=many {\n  [seeker] x\n}\n", PromptMode, Type)
		if err.Class() != 'A' {
			t.Errorf("class = %c, want A", err.Class())
		}
	})
}

func TestBindingNameErrors(t *testing.T) {
	t.Run("U2-T15_dash_in_name", func(t *testing.T) {
		mustFail(t, "$my-var = \"x\"\n", PromptMode, Syntax)
	})
	t.Run("U2-T15_keyword_name", func(t *testing.T) {
		for _, kw := range []string{"parallel", "foreach", "loop", "success", "error",
			"info", "each", "limit", "notify", "clarify", "null"} {
			err := mustFail(t, "$"+kw+" = \"x\"\n", PromptMode, Syntax)
			if !strings.Contains(err.Msg, "reserved") {
				t.Errorf("$%s: msg = %q, want reserved keyword mention", kw, err.Msg)
			}
		}
	})
}

func TestUnclosedBlocks(t *testing.T) {
	t.Run("U2-T16_unclosed_handler", func(t *testing.T) {
		err := mustFail(t, string(readTestdata(t, "unclosed_block.ann")), ProgramMode, Syntax)
		if err.Line != 3 {
			t.Errorf("line = %d, want 3 (handler opening line)", err.Line)
		}
		if !strings.Contains(err.Msg, "unclosed") {
			t.Errorf("msg = %q, want unclosed mention", err.Msg)
		}
	})
	t.Run("U2-T16_unclosed_foreach", func(t *testing.T) {
		err := mustFail(t, "foreach $l {\n  [seeker] x\n", PromptMode, Syntax)
		if err.Line != 1 {
			t.Errorf("line = %d, want 1", err.Line)
		}
	})
	t.Run("U2-T16_unclosed_parallel", func(t *testing.T) {
		err := mustFail(t, "parallel {\n  [seeker] --id=a x\n", PromptMode, Syntax)
		if err.Line != 1 {
			t.Errorf("line = %d, want 1", err.Line)
		}
	})
	t.Run("U2-T16_stray_close", func(t *testing.T) {
		mustFail(t, "[seeker] x\n}\n", PromptMode, Syntax)
	})
}

func TestStopOnFirstError(t *testing.T) {
	t.Run("U2-T17_first_error_only", func(t *testing.T) {
		src := "[seeker] ok\n$my-var = \"bad\"\n[also] broken (\n"
		prog, err := Parse([]byte(src), PromptMode)
		if err == nil {
			t.Fatal("Parse succeeded, want error")
		}
		if prog != nil {
			t.Fatal("Parse returned partial program alongside error (§7.2)")
		}
		if err.Line != 2 {
			t.Errorf("line = %d, want 2 (first error)", err.Line)
		}
	})
}

func TestLexerErrors(t *testing.T) {
	cases := map[string]string{
		"unterminated_command":  "[seeker one\n",
		"invalid_command_name":  "[bad name] x\n",
		"unterminated_string":   "$s = \"open\n",
		"empty_binding":         "$ = \"x\"\n",
		"empty_flag":            "[seeker] -- x\n",
		"colon_no_space":        "[seeker] x :y\n",
		"unclosed_list":         "$l = list(\"a\"\n",
		"bad_list_element":      "$l = list(x)\n",
		"duplicate_handler":     "[a] x\n  success -> {}\n  success -> {}\n",
		"stray_handler":         "success -> {}\n",
		"bad_loop_header":       "loop {\n}\n",
		"bad_foreach_header":    "foreach items {\n}\n",
		"bad_parallel_header":   "parallel now {\n}\n",
		"non_dispatch_in_par":   "parallel {\n  $x = \"1\"\n}\n",
		"missing_expr":          "$x =\n",
		"bad_expr_start":        "$x = )\n",
		"extra_after_string":    "$x = \"a\" b\n",
		"extra_after_list":      "$l = list(\"a\") b\n",
		"unexpected_char":       "[seeker] (\n",
		"bad_token_in_dispatch": "[seeker] = x\n",
		"inline_handler_body":   "[a] x\n  success -> { [notify] y }\n",
		"handler_no_brace":      "[a] x\n  success ->\n",
		"each_no_brace":         "parallel {\n  [seeker] --id=a x\n}\n  each ->\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			mustFail(t, src, PromptMode, Syntax)
		})
	}
}

func TestErrorFormatting(t *testing.T) {
	err := &ParseError{Line: 3, Col: 7, Category: UnknownCommand, Msg: "unknown command [x]"}
	if got := err.Error(); !strings.Contains(got, "3:7") || !strings.Contains(got, "unknown command") {
		t.Errorf("Error() = %q", got)
	}
	for cat, want := range map[Category]string{
		Syntax: "syntax error", UnknownCommand: "unknown command",
		Type: "type error", VersionMismatch: "version mismatch",
		Category(99): "unknown category",
	} {
		if cat.String() != want {
			t.Errorf("Category(%d).String() = %q, want %q", cat, cat.String(), want)
		}
	}
}
