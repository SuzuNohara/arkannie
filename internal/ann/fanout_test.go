package ann

import (
	"strings"
	"testing"
)

// TestParallelForeachParse covers the valid dynamic fan-out form: it parses to a
// *ParallelForeach with the list ref (dot-path preserved without $), the id base,
// and exactly one template dispatch carrying no --id of its own (R9, R11).
func TestParallelForeachParse(t *testing.T) {
	src := "parallel foreach $r.items --id=W {\n  [agent] : do $item now\n}\n"
	prog := mustParse(t, src, PromptMode)
	pf, ok := prog.Statements[0].(*ParallelForeach)
	if !ok {
		t.Fatalf("statement 0 is %T, want *ParallelForeach", prog.Statements[0])
	}
	if pf.List != "r.items" {
		t.Errorf("List = %q, want r.items", pf.List)
	}
	if pf.BaseID != "W" {
		t.Errorf("BaseID = %q, want W", pf.BaseID)
	}
	if pf.Template.Command != "agent" {
		t.Errorf("template command = %q, want agent", pf.Template.Command)
	}
	if pf.Template.ID != "" {
		t.Errorf("template must not carry an id, got %q", pf.Template.ID)
	}
	if pf.Template.Context != "do $item now" {
		t.Errorf("template context = %q", pf.Template.Context)
	}
	if pf.Each != nil {
		t.Errorf("Each should be nil when no each block is present, got %v", pf.Each)
	}
	// The dump renders the new node (exercised via dumpProgram).
	if dump := dumpProgram(prog); !strings.Contains(dump, "ParallelForeach") {
		t.Errorf("dump does not mention ParallelForeach:\n%s", dump)
	}
}

// TestParallelForeachNoDotPath: a plain (non-dotted) list ref parses too.
func TestParallelForeachNoDotPath(t *testing.T) {
	pf := parseFanout(t, "parallel foreach $items --id=W {\n  [agent] : \"$item\"\n}\n")
	if pf.List != "items" {
		t.Errorf("List = %q, want items", pf.List)
	}
}

// TestParallelForeachEach: the optional each -> {} handler is captured.
func TestParallelForeachEach(t *testing.T) {
	src := "parallel foreach $items --id=W {\n  [agent] : \"$item\"\n}\n" +
		"  each -> {\n    [notify] : \"done\"\n  }\n"
	pf := parseFanout(t, src)
	if pf.Each == nil {
		t.Fatal("Each should be parsed when an each -> {} block follows")
	}
	if len(pf.Each) != 1 {
		t.Errorf("Each has %d statements, want 1", len(pf.Each))
	}
}

// TestParallelForeachOneTemplate: exactly one dispatch template is required — a
// body with zero or two dispatches is a Syntax error.
func TestParallelForeachOneTemplate(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		mustFail(t, "parallel foreach $items --id=W {\n}\n", PromptMode, Syntax)
	})
	t.Run("two", func(t *testing.T) {
		src := "parallel foreach $items --id=W {\n  [a] : \"1\"\n  [b] : \"2\"\n}\n"
		mustFail(t, src, PromptMode, Syntax)
	})
}

// TestParallelForeachIDRequired: the --id base is mandatory.
func TestParallelForeachIDRequired(t *testing.T) {
	mustFail(t, "parallel foreach $items {\n  [a] : \"$item\"\n}\n", PromptMode, Syntax)
}

// TestParallelForeachTemplateNoID: the template must not carry its own --id (the
// runtime synthesizes <base>-<n>).
func TestParallelForeachTemplateNoID(t *testing.T) {
	src := "parallel foreach $items --id=W {\n  [a] --id=z : \"$item\"\n}\n"
	mustFail(t, src, PromptMode, Syntax)
}

// TestParallelForeachHeaderExtraFlag: only --id is allowed in the header.
func TestParallelForeachHeaderExtraFlag(t *testing.T) {
	src := "parallel foreach $items --id=W --foo {\n  [a] : \"$item\"\n}\n"
	mustFail(t, src, PromptMode, Syntax)
}

// TestParallelForeachPrefixCollision covers R13 prefix reservation: no literal
// dispatch --id in the program may match ^<base>-[0-9]+$ of any fan-out, in
// either textual order.
func TestParallelForeachPrefixCollision(t *testing.T) {
	t.Run("literal_after_fanout", func(t *testing.T) {
		src := "parallel foreach $items --id=W {\n  [a] : \"$item\"\n}\n" +
			"[b] --id=W-1 : \"y\"\n"
		mustFail(t, src, PromptMode, Syntax)
	})
	t.Run("literal_before_fanout", func(t *testing.T) {
		src := "[b] --id=W-2 : \"y\"\n" +
			"parallel foreach $items --id=W {\n  [a] : \"$item\"\n}\n"
		mustFail(t, src, PromptMode, Syntax)
	})
	t.Run("collision_inside_static_parallel", func(t *testing.T) {
		src := "parallel foreach $items --id=W {\n  [a] : \"$item\"\n}\n" +
			"parallel {\n  [b] --id=W-9 : \"y\"\n}\n"
		mustFail(t, src, PromptMode, Syntax)
	})
}

// TestParallelForeachNoFalseCollision: ids that are not <base>-<digits> do not
// collide with the reserved prefix and must still parse.
func TestParallelForeachNoFalseCollision(t *testing.T) {
	t.Run("no_dash", func(t *testing.T) {
		parseFanout(t, "parallel foreach $items --id=W {\n  [a] : \"$item\"\n}\n"+
			"[b] --id=W1 : \"y\"\n")
	})
	t.Run("non_numeric_suffix", func(t *testing.T) {
		parseFanout(t, "parallel foreach $items --id=W {\n  [a] : \"$item\"\n}\n"+
			"[b] --id=W-a : \"y\"\n")
	})
	t.Run("different_base", func(t *testing.T) {
		parseFanout(t, "parallel foreach $items --id=W {\n  [a] : \"$item\"\n}\n"+
			"[b] --id=V-1 : \"y\"\n")
	})
	t.Run("return_id_not_a_dispatch_id", func(t *testing.T) {
		// A [return] --id may match the pattern without colliding: only dispatch
		// ids reserve against a fan-out prefix.
		parseFanout(t, "parallel foreach $items --id=W {\n  [a] : \"$item\"\n}\n"+
			"[return] --id=W-1 \"z\"\n")
	})
}

// TestParallelForeachStaticRegression: plain parallel {} is untouched by the
// dynamic fan-out branch.
func TestParallelForeachStaticRegression(t *testing.T) {
	src := "parallel {\n  [a] --id=x : \"1\"\n  [b] --id=y : \"2\"\n}\n"
	prog := mustParse(t, src, PromptMode)
	par, ok := prog.Statements[0].(*Parallel)
	if !ok {
		t.Fatalf("statement 0 is %T, want *Parallel", prog.Statements[0])
	}
	if len(par.Dispatches) != 2 {
		t.Errorf("static parallel dispatches = %d, want 2", len(par.Dispatches))
	}
}

// TestForeachStillFreeStanding: a bare foreach keyword still parses as sequential
// iteration — only `parallel foreach` triggers the fan-out.
func TestForeachStillFreeStanding(t *testing.T) {
	prog := mustParse(t, "foreach $items {\n  [a] : \"$item\"\n}\n", PromptMode)
	if _, ok := prog.Statements[0].(*Foreach); !ok {
		t.Fatalf("statement 0 is %T, want *Foreach", prog.Statements[0])
	}
}

// TestParallelForeachMalformedHeader covers the header error branches.
func TestParallelForeachMalformedHeader(t *testing.T) {
	t.Run("no_brace", func(t *testing.T) {
		mustFail(t, "parallel foreach $items --id=W\n", PromptMode, Syntax)
	})
	t.Run("non_flag_before_brace", func(t *testing.T) {
		mustFail(t, "parallel foreach $items extra {\n  [a] : \"$item\"\n}\n", PromptMode, Syntax)
	})
	t.Run("no_list_binding", func(t *testing.T) {
		mustFail(t, "parallel foreach {\n  [a] : \"$item\"\n}\n", PromptMode, Syntax)
	})
}

// TestParallelForeachMalformedBody covers the body error branches.
func TestParallelForeachMalformedBody(t *testing.T) {
	t.Run("unclosed", func(t *testing.T) {
		mustFail(t, "parallel foreach $items --id=W {\n  [a] : \"$item\"\n", PromptMode, Syntax)
	})
	t.Run("non_command_line", func(t *testing.T) {
		mustFail(t, "parallel foreach $items --id=W {\n  notacommand\n}\n", PromptMode, Syntax)
	})
}

func parseFanout(t *testing.T, src string) *ParallelForeach {
	t.Helper()
	prog := mustParse(t, src, PromptMode)
	pf, ok := prog.Statements[0].(*ParallelForeach)
	if !ok {
		t.Fatalf("statement 0 is %T, want *ParallelForeach", prog.Statements[0])
	}
	return pf
}
