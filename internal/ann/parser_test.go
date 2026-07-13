package ann

import (
	"os"
	"path/filepath"
	"testing"
)

const testdataDir = "../../testdata/ann"

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdataDir, name))
	if err != nil {
		t.Fatalf("reading testdata %s: %v", name, err)
	}
	return data
}

func mustParse(t *testing.T, src string, mode Mode) *Program {
	t.Helper()
	prog, err := Parse([]byte(src), mode)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	return prog
}

func mustFail(t *testing.T, src string, mode Mode, want Category) *ParseError {
	t.Helper()
	prog, err := Parse([]byte(src), mode)
	if err == nil {
		t.Fatalf("Parse succeeded, want %v error", want)
	}
	if prog != nil {
		t.Fatalf("Parse returned a partial program alongside the error (§7.2)")
	}
	if err.Category != want {
		t.Fatalf("error category = %v, want %v (err: %v)", err.Category, want, err)
	}
	return err
}

func firstDispatch(t *testing.T, prog *Program) *Dispatch {
	t.Helper()
	if len(prog.Statements) == 0 {
		t.Fatal("program has no statements")
	}
	d, ok := prog.Statements[0].(*Dispatch)
	if !ok {
		t.Fatalf("statement 0 is %T, want *Dispatch", prog.Statements[0])
	}
	return d
}

func TestParseGolden(t *testing.T) {
	t.Run("U2-T1_all_constructs_golden", func(t *testing.T) {
		src := readTestdata(t, "all_constructs.ann")
		prog, err := Parse(src, ProgramMode)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		got := dumpProgram(prog)
		want := string(readTestdata(t, "all_constructs.golden"))
		if got != want {
			t.Errorf("AST dump mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

func TestVersionHeader(t *testing.T) {
	t.Run("U2-T2_program_header_col0_ok", func(t *testing.T) {
		prog := mustParse(t, "# ann v0.2\n[seeker] auth\n", ProgramMode)
		if len(prog.Statements) != 1 {
			t.Fatalf("statements = %d, want 1", len(prog.Statements))
		}
	})
	t.Run("U2-T2_program_header_after_comments_ok", func(t *testing.T) {
		prog := mustParse(t, "// preamble\n\n# ann v0.2\n[seeker] auth\n", ProgramMode)
		if firstDispatch(t, prog).Line != 4 {
			t.Fatalf("dispatch line = %d, want 4", firstDispatch(t, prog).Line)
		}
	})
	t.Run("U2-T2_prompt_no_header_ok", func(t *testing.T) {
		prog := mustParse(t, "[seeker] auth\n", PromptMode)
		if firstDispatch(t, prog).Command != "seeker" {
			t.Fatal("expected [seeker] dispatch")
		}
	})
	t.Run("U2-T2_prompt_header_ignored", func(t *testing.T) {
		prog := mustParse(t, "# ann v0.2\n[seeker] auth\n", PromptMode)
		if len(prog.Statements) != 1 {
			t.Fatalf("statements = %d, want 1", len(prog.Statements))
		}
	})
	t.Run("U2-T2_prompt_other_header_ignored", func(t *testing.T) {
		prog := mustParse(t, "# ann v0.9\n[seeker] auth\n", PromptMode)
		if len(prog.Statements) != 1 {
			t.Fatalf("statements = %d, want 1", len(prog.Statements))
		}
	})
}

func TestContextBlocks(t *testing.T) {
	t.Run("U2-T3_single_line", func(t *testing.T) {
		prog := mustParse(t, "[activity] act-001 --new : refactor auth middleware\n", PromptMode)
		d := firstDispatch(t, prog)
		if d.Context != "refactor auth middleware" {
			t.Errorf("ctx = %q", d.Context)
		}
	})
	t.Run("U2-T3_multiline_ends_blank", func(t *testing.T) {
		src := "[activity] act-001 :\n  line one\n  line two\n\n[notify] after\n"
		prog := mustParse(t, src, PromptMode)
		d := firstDispatch(t, prog)
		if d.Context != "line one\nline two" {
			t.Errorf("ctx = %q", d.Context)
		}
		if len(prog.Statements) != 2 {
			t.Fatalf("statements = %d, want 2", len(prog.Statements))
		}
	})
	t.Run("U2-T3_multiline_ends_arrow", func(t *testing.T) {
		src := "[activity] act-001 :\n  line one\n  line two\n  success -> {\n    [notify] ok\n  }\n"
		prog := mustParse(t, src, PromptMode)
		d := firstDispatch(t, prog)
		if d.Context != "line one\nline two" {
			t.Errorf("ctx = %q", d.Context)
		}
		if len(d.Handlers[StatusSuccess]) != 1 {
			t.Fatalf("success handler missing after context block")
		}
	})
}

func TestFlagsAndArgs(t *testing.T) {
	t.Run("U2-T4_flags_and_positional_args", func(t *testing.T) {
		prog := mustParse(t, "[seeker] one two --bool --val=x --id=me three\n", PromptMode)
		d := firstDispatch(t, prog)
		wantArgs := []string{"one", "two", "three"}
		if len(d.Args) != len(wantArgs) {
			t.Fatalf("args = %q, want %q", d.Args, wantArgs)
		}
		for i := range wantArgs {
			if d.Args[i] != wantArgs[i] {
				t.Errorf("args[%d] = %q, want %q", i, d.Args[i], wantArgs[i])
			}
		}
		if v, ok := d.Flags["bool"]; !ok || v != "" {
			t.Errorf("bool flag = %q,%v — want \"\",true", v, ok)
		}
		if d.Flags["val"] != "x" {
			t.Errorf("val flag = %q, want x", d.Flags["val"])
		}
		if d.ID != "me" || d.Flags["id"] != "me" {
			t.Errorf("id = %q flags[id] = %q, want me", d.ID, d.Flags["id"])
		}
	})
}

func TestBindings(t *testing.T) {
	t.Run("U2-T5_binding_from_command", func(t *testing.T) {
		prog := mustParse(t, "$r = [seeker] q --deep\n", PromptMode)
		a := prog.Statements[0].(*Assign)
		if a.Name != "r" {
			t.Errorf("name = %q, want r", a.Name)
		}
		d, ok := a.Expr.(*Dispatch)
		if !ok {
			t.Fatalf("expr is %T, want *Dispatch", a.Expr)
		}
		if d.Command != "seeker" || d.Flags["deep"] != "" {
			t.Errorf("dispatch = %+v", d)
		}
	})
	t.Run("U2-T5_binding_literal", func(t *testing.T) {
		prog := mustParse(t, "$s = \"hello world\"\n", PromptMode)
		lit, ok := prog.Statements[0].(*Assign).Expr.(StrLit)
		if !ok || lit.Value != "hello world" {
			t.Fatalf("expr = %#v, want StrLit hello world", prog.Statements[0].(*Assign).Expr)
		}
	})
	t.Run("U2-T5_binding_list", func(t *testing.T) {
		prog := mustParse(t, "$l = list(\"a\", $b, \"c\")\n", PromptMode)
		lst, ok := prog.Statements[0].(*Assign).Expr.(ListLit)
		if !ok {
			t.Fatalf("expr is not ListLit")
		}
		want := []string{"a", "$b", "c"}
		if len(lst.Elems) != 3 {
			t.Fatalf("elems = %q, want %q", lst.Elems, want)
		}
		for i := range want {
			if lst.Elems[i] != want[i] {
				t.Errorf("elems[%d] = %q, want %q", i, lst.Elems[i], want[i])
			}
		}
	})
}

func TestHandlers(t *testing.T) {
	t.Run("U2-T6_no_handlers", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[seeker] q\n", PromptMode))
		if d.Handlers != nil {
			t.Errorf("handlers = %v, want nil", d.Handlers)
		}
	})
	t.Run("U2-T6_three_handlers_nested_bindings", func(t *testing.T) {
		src := "[a] one\n" +
			"  success -> {\n" +
			"    $x = \"1\"\n" +
			"    [b] two\n" +
			"      error -> {\n" +
			"        $y = \"2\"\n" +
			"      }\n" +
			"  }\n" +
			"  error -> {\n" +
			"    [ask-user] retry\n" +
			"  }\n" +
			"  info -> {}\n"
		d := firstDispatch(t, mustParse(t, src, PromptMode))
		if len(d.Handlers) != 3 {
			t.Fatalf("handlers = %d, want 3", len(d.Handlers))
		}
		succ := d.Handlers[StatusSuccess]
		if len(succ) != 2 {
			t.Fatalf("success body = %d stmts, want 2", len(succ))
		}
		nested := succ[1].(*Dispatch)
		inner := nested.Handlers[StatusError]
		if len(inner) != 1 {
			t.Fatalf("nested error handler = %d stmts, want 1", len(inner))
		}
		if inner[0].(*Assign).Name != "y" {
			t.Errorf("nested binding = %q, want y", inner[0].(*Assign).Name)
		}
		if len(d.Handlers[StatusInfo]) != 0 {
			t.Errorf("info body = %d stmts, want 0", len(d.Handlers[StatusInfo]))
		}
	})
	t.Run("U2-T6_binding_dispatch_with_handlers", func(t *testing.T) {
		src := "$r = [seeker] q\n  success -> {\n    [notify] ok\n  }\n"
		prog := mustParse(t, src, PromptMode)
		d := prog.Statements[0].(*Assign).Expr.(*Dispatch)
		if len(d.Handlers[StatusSuccess]) != 1 {
			t.Fatal("handler not attached to binding dispatch")
		}
	})
}

func TestParallelValid(t *testing.T) {
	t.Run("U2-T7_unique_ids_and_each", func(t *testing.T) {
		src := "parallel {\n" +
			"  [seeker] --id=a one\n" +
			"  [reviewer] --id=b two\n" +
			"}\n" +
			"  each -> {\n" +
			"    [notify] $result\n" +
			"  }\n"
		prog := mustParse(t, src, PromptMode)
		par := prog.Statements[0].(*Parallel)
		if len(par.Dispatches) != 2 {
			t.Fatalf("dispatches = %d, want 2", len(par.Dispatches))
		}
		if par.Dispatches[0].ID != "a" || par.Dispatches[1].ID != "b" {
			t.Errorf("ids = %q,%q — want a,b", par.Dispatches[0].ID, par.Dispatches[1].ID)
		}
		if len(par.Each) != 1 {
			t.Fatalf("each body = %d stmts, want 1", len(par.Each))
		}
	})
	t.Run("U2-T7_no_each_ok", func(t *testing.T) {
		src := "parallel {\n  [seeker] --id=a one\n}\n"
		par := mustParse(t, src, PromptMode).Statements[0].(*Parallel)
		if par.Each != nil {
			t.Errorf("each = %v, want nil", par.Each)
		}
	})
}

func TestForeachLoop(t *testing.T) {
	t.Run("U2-T8_foreach", func(t *testing.T) {
		src := "foreach $items {\n  [seeker] $item\n}\n"
		fe := mustParse(t, src, PromptMode).Statements[0].(*Foreach)
		if fe.List != "items" {
			t.Errorf("list = %q, want items", fe.List)
		}
		if len(fe.Body) != 1 {
			t.Fatalf("body = %d stmts, want 1", len(fe.Body))
		}
		if fe.Body[0].(*Dispatch).Args[0] != "$item" {
			t.Errorf("arg = %q, want $item", fe.Body[0].(*Dispatch).Args[0])
		}
	})
	t.Run("U2-T8_loop_limit_n", func(t *testing.T) {
		src := "loop limit=4 {\n  [seeker] again\n}\n"
		lp := mustParse(t, src, PromptMode).Statements[0].(*Loop)
		if lp.Limit != 4 {
			t.Errorf("limit = %d, want 4", lp.Limit)
		}
		if len(lp.Body) != 1 {
			t.Fatalf("body = %d stmts, want 1", len(lp.Body))
		}
	})
}

func TestLoopUntil(t *testing.T) {
	t.Run("ref_dot_eq_string", func(t *testing.T) {
		src := "loop limit=3 until $r.status == \"success\" {\n  [seeker] again\n}\n"
		lp := mustParse(t, src, PromptMode).Statements[0].(*Loop)
		if lp.Limit != 3 {
			t.Errorf("limit = %d, want 3", lp.Limit)
		}
		if lp.Until == nil {
			t.Fatal("until = nil, want a guard")
		}
		if !lp.Until.Left.IsRef || lp.Until.Left.Text != "r.status" {
			t.Errorf("until.left = %+v, want ref r.status", lp.Until.Left)
		}
		if lp.Until.Op != "==" {
			t.Errorf("until.op = %q, want ==", lp.Until.Op)
		}
		if lp.Until.Right.IsRef || lp.Until.Right.IsNull || lp.Until.Right.Text != "success" {
			t.Errorf("until.right = %+v, want string success", lp.Until.Right)
		}
		if len(lp.Body) != 1 {
			t.Fatalf("body = %d stmts, want 1", len(lp.Body))
		}
	})
	t.Run("ne_operator", func(t *testing.T) {
		src := "loop limit=2 until $x != \"done\" {\n  [seeker] a\n}\n"
		lp := mustParse(t, src, PromptMode).Statements[0].(*Loop)
		if lp.Until == nil || lp.Until.Op != "!=" {
			t.Errorf("until = %+v, want op !=", lp.Until)
		}
	})
	t.Run("null_operand", func(t *testing.T) {
		src := "loop limit=5 until $r.result != null {\n  [seeker] a\n}\n"
		lp := mustParse(t, src, PromptMode).Statements[0].(*Loop)
		if lp.Until == nil || !lp.Until.Right.IsNull || lp.Until.Right.IsRef {
			t.Errorf("until.right = %+v, want null", lp.Until)
		}
	})
	t.Run("ref_without_dot", func(t *testing.T) {
		src := "loop limit=2 until $x == \"y\" {\n  [seeker] a\n}\n"
		lp := mustParse(t, src, PromptMode).Statements[0].(*Loop)
		if lp.Until == nil || !lp.Until.Left.IsRef || lp.Until.Left.Text != "x" {
			t.Errorf("until.left = %+v, want ref x", lp.Until)
		}
	})
	t.Run("without_until_regression", func(t *testing.T) {
		src := "loop limit=4 {\n  [seeker] again\n}\n"
		lp := mustParse(t, src, PromptMode).Statements[0].(*Loop)
		if lp.Limit != 4 {
			t.Errorf("limit = %d, want 4", lp.Limit)
		}
		if lp.Until != nil {
			t.Errorf("until = %+v, want nil (no until clause)", lp.Until)
		}
		if len(lp.Body) != 1 {
			t.Fatalf("body = %d stmts, want 1", len(lp.Body))
		}
	})
	t.Run("nested_in_foreach", func(t *testing.T) {
		src := "foreach $items {\n  loop limit=2 until $item != null {\n    [seeker] $item\n  }\n}\n"
		fe := mustParse(t, src, PromptMode).Statements[0].(*Foreach)
		lp, ok := fe.Body[0].(*Loop)
		if !ok {
			t.Fatalf("body[0] = %T, want *Loop", fe.Body[0])
		}
		if lp.Until == nil || !lp.Until.Left.IsRef || lp.Until.Left.Text != "item" {
			t.Errorf("until = %+v, want ref item != null", lp.Until)
		}
	})
}

func TestLoopUntilDump(t *testing.T) {
	src := "loop limit=3 until $r.status == \"ok\" {\n  [notify] good\n}\n"
	got := dumpProgram(mustParse(t, src, PromptMode))
	want := "Program\n" +
		"  Loop line=1 limit=3 until op=== left=$r.status right=\"ok\"\n" +
		"    Dispatch line=2 cmd=notify args=[\"good\"] id=\"\" ctx=\"\" flags={}\n"
	if got != want {
		t.Errorf("dump mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestIfStatements(t *testing.T) {
	t.Run("ref_dot_eq_string_then_only", func(t *testing.T) {
		src := "if $x.status == \"ok\" {\n  [notify] good\n}\n"
		iff := mustParse(t, src, PromptMode).Statements[0].(*If)
		if !iff.Left.IsRef || iff.Left.Text != "x.status" {
			t.Errorf("left = %+v, want ref x.status", iff.Left)
		}
		if iff.Op != "==" {
			t.Errorf("op = %q, want ==", iff.Op)
		}
		if iff.Right.IsRef || iff.Right.IsNull || iff.Right.Text != "ok" {
			t.Errorf("right = %+v, want string ok", iff.Right)
		}
		if len(iff.Then) != 1 {
			t.Fatalf("then = %d stmts, want 1", len(iff.Then))
		}
		if iff.Else != nil {
			t.Errorf("else = %v, want nil", iff.Else)
		}
		if iff.Line != 1 {
			t.Errorf("line = %d, want 1", iff.Line)
		}
	})
	t.Run("with_else_block", func(t *testing.T) {
		src := "if $x == \"ok\" {\n  [notify] good\n}\nelse {\n  [notify] bad\n}\n"
		iff := mustParse(t, src, PromptMode).Statements[0].(*If)
		if len(iff.Then) != 1 || len(iff.Else) != 1 {
			t.Fatalf("then=%d else=%d, want 1/1", len(iff.Then), len(iff.Else))
		}
	})
	t.Run("ne_operator", func(t *testing.T) {
		src := "if $x != \"ok\" {\n  [notify] n\n}\n"
		iff := mustParse(t, src, PromptMode).Statements[0].(*If)
		if iff.Op != "!=" {
			t.Errorf("op = %q, want !=", iff.Op)
		}
	})
	t.Run("ref_without_dot", func(t *testing.T) {
		src := "if $x == \"y\" {\n  [notify] n\n}\n"
		iff := mustParse(t, src, PromptMode).Statements[0].(*If)
		if !iff.Left.IsRef || iff.Left.Text != "x" {
			t.Errorf("left = %+v, want ref x", iff.Left)
		}
	})
	t.Run("null_operand", func(t *testing.T) {
		src := "if $x.status != null {\n  [notify] n\n}\n"
		iff := mustParse(t, src, PromptMode).Statements[0].(*If)
		if !iff.Right.IsNull || iff.Right.IsRef {
			t.Errorf("right = %+v, want null", iff.Right)
		}
	})
	t.Run("string_both_sides", func(t *testing.T) {
		src := "if \"a\" == \"b\" {\n  [notify] n\n}\n"
		iff := mustParse(t, src, PromptMode).Statements[0].(*If)
		if iff.Left.IsRef || iff.Left.Text != "a" || iff.Right.Text != "b" {
			t.Errorf("operands = %+v / %+v", iff.Left, iff.Right)
		}
	})
	t.Run("empty_then_block", func(t *testing.T) {
		src := "if $x == \"y\" {\n}\n"
		iff := mustParse(t, src, PromptMode).Statements[0].(*If)
		if len(iff.Then) != 0 {
			t.Errorf("then = %d, want 0", len(iff.Then))
		}
	})
	t.Run("nested_in_foreach", func(t *testing.T) {
		src := "foreach $items {\n  if $item != null {\n    [notify] $item\n  }\n}\n"
		fe := mustParse(t, src, PromptMode).Statements[0].(*Foreach)
		if _, ok := fe.Body[0].(*If); !ok {
			t.Fatalf("body[0] = %T, want *If", fe.Body[0])
		}
	})
	t.Run("nested_if_in_then", func(t *testing.T) {
		src := "if $a == \"1\" {\n  if $b == \"2\" {\n    [notify] deep\n  }\n}\n"
		outer := mustParse(t, src, PromptMode).Statements[0].(*If)
		if _, ok := outer.Then[0].(*If); !ok {
			t.Fatalf("then[0] = %T, want *If", outer.Then[0])
		}
	})
}

func TestDottedRefs(t *testing.T) {
	t.Run("R2-T05b_dotted_arg_is_one_ref", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[echo] $x.campo\n", PromptMode))
		if len(d.Args) != 1 || d.Args[0] != "$x.campo" {
			t.Errorf("args = %q, want [$x.campo] (a dotted arg must be a single ref)", d.Args)
		}
	})
	t.Run("R2-T05b_deep_dotted_arg", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[echo] $x.a.b.c\n", PromptMode))
		if len(d.Args) != 1 || d.Args[0] != "$x.a.b.c" {
			t.Errorf("args = %q, want [$x.a.b.c]", d.Args)
		}
	})
	t.Run("R2-T05b_dotted_arg_among_plain_args", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[echo] one $x.f two\n", PromptMode))
		want := []string{"one", "$x.f", "two"}
		if len(d.Args) != len(want) {
			t.Fatalf("args = %q, want %q", d.Args, want)
		}
		for i := range want {
			if d.Args[i] != want[i] {
				t.Errorf("args[%d] = %q, want %q", i, d.Args[i], want[i])
			}
		}
	})
	t.Run("R2-T05b_return_dotted_operand_is_one_ref", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[return] --id=r $result.payload.out\n", PromptMode))
		if d.ID != "r" {
			t.Errorf("id = %q, want r", d.ID)
		}
		if len(d.Args) != 1 || d.Args[0] != "$result.payload.out" {
			t.Errorf("args = %q, want [$result.payload.out] (must NOT split into two tokens)", d.Args)
		}
	})
	t.Run("R2-T05b_foreach_dotted_list", func(t *testing.T) {
		src := "foreach $r.items {\n  [seeker] $item\n}\n"
		fe := mustParse(t, src, PromptMode).Statements[0].(*Foreach)
		if fe.List != "r.items" {
			t.Errorf("list = %q, want r.items", fe.List)
		}
		if len(fe.Body) != 1 {
			t.Fatalf("body = %d stmts, want 1", len(fe.Body))
		}
	})
	t.Run("R2-T05b_list_dotted_element", func(t *testing.T) {
		lst := mustParse(t, "$l = list($x.a, \"b\", $y.p.q)\n", PromptMode).
			Statements[0].(*Assign).Expr.(ListLit)
		want := []string{"$x.a", "b", "$y.p.q"}
		if len(lst.Elems) != len(want) {
			t.Fatalf("elems = %q, want %q", lst.Elems, want)
		}
		for i := range want {
			if lst.Elems[i] != want[i] {
				t.Errorf("elems[%d] = %q, want %q", i, lst.Elems[i], want[i])
			}
		}
	})
	t.Run("R2-T05b_regression_plain_ref_arg", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[echo] $x\n", PromptMode))
		if len(d.Args) != 1 || d.Args[0] != "$x" {
			t.Errorf("args = %q, want [$x]", d.Args)
		}
	})
	t.Run("R2-T05b_regression_plain_foreach_list", func(t *testing.T) {
		fe := mustParse(t, "foreach $items {\n  [seeker] $item\n}\n", PromptMode).
			Statements[0].(*Foreach)
		if fe.List != "items" {
			t.Errorf("list = %q, want items", fe.List)
		}
	})
	t.Run("R2-T05b_regression_dot_in_context_text_verbatim", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[activity] a : uses $x.y verbatim\n", PromptMode))
		if d.Context != "uses $x.y verbatim" {
			t.Errorf("ctx = %q, want the dotted ref kept verbatim in context text", d.Context)
		}
	})
	t.Run("R2-T05b_regression_if_operand_dotted", func(t *testing.T) {
		iff := mustParse(t, "if $x.status == \"ok\" {\n  [notify] good\n}\n", PromptMode).
			Statements[0].(*If)
		if !iff.Left.IsRef || iff.Left.Text != "x.status" {
			t.Errorf("left = %+v, want ref x.status", iff.Left)
		}
	})
}

func TestKeywordsAsFreeText(t *testing.T) {
	t.Run("if_else_until_as_positional_args", func(t *testing.T) {
		prog := mustParse(t, "[notify] if else until\n", PromptMode)
		d := firstDispatch(t, prog)
		want := []string{"if", "else", "until"}
		if len(d.Args) != 3 {
			t.Fatalf("args = %q, want %q", d.Args, want)
		}
		for i := range want {
			if d.Args[i] != want[i] {
				t.Errorf("args[%d] = %q, want %q", i, d.Args[i], want[i])
			}
		}
	})
	t.Run("keywords_inside_multiline_context", func(t *testing.T) {
		src := "[activity] a :\n  if the build fails\n  else retry until green\n\n[notify] done\n"
		prog := mustParse(t, src, PromptMode)
		d := firstDispatch(t, prog)
		if d.Context != "if the build fails\nelse retry until green" {
			t.Errorf("ctx = %q", d.Context)
		}
		if len(prog.Statements) != 2 {
			t.Fatalf("statements = %d, want 2", len(prog.Statements))
		}
	})
}

func TestIfDump(t *testing.T) {
	src := "if $x.status == \"ok\" {\n  [notify] good\n}\nelse {\n  [notify] bad\n}\n"
	prog := mustParse(t, src, PromptMode)
	got := dumpProgram(prog)
	want := "Program\n" +
		"  If line=1 op=== left=$x.status right=\"ok\"\n" +
		"    Then\n" +
		"      Dispatch line=2 cmd=notify args=[\"good\"] id=\"\" ctx=\"\" flags={}\n" +
		"    Else\n" +
		"      Dispatch line=5 cmd=notify args=[\"bad\"] id=\"\" ctx=\"\" flags={}\n"
	if got != want {
		t.Errorf("dump mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestCommentsAndTemplates(t *testing.T) {
	t.Run("U2-T9_comments_everywhere_strings_verbatim", func(t *testing.T) {
		src := "// top comment\n" +
			"[a] one // trailing comment\n" +
			"$s = \"{{ slot }} and $ref stay // verbatim\"\n" +
			"// between statements\n" +
			"[b] $s\n" +
			"// bottom\n"
		prog := mustParse(t, src, PromptMode)
		if len(prog.Statements) != 3 {
			t.Fatalf("statements = %d, want 3", len(prog.Statements))
		}
		d := prog.Statements[0].(*Dispatch)
		if len(d.Args) != 1 || d.Args[0] != "one" {
			t.Errorf("args = %q, want [one] (trailing comment must be stripped)", d.Args)
		}
		lit := prog.Statements[1].(*Assign).Expr.(StrLit)
		if lit.Value != "{{ slot }} and $ref stay // verbatim" {
			t.Errorf("string = %q — templates and $refs must stay intact", lit.Value)
		}
		if prog.Statements[2].(*Dispatch).Args[0] != "$s" {
			t.Errorf("binding arg lost: %q", prog.Statements[2].(*Dispatch).Args)
		}
	})
}
