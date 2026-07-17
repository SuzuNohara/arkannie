package ann

import (
	"strings"
	"testing"
)

// TestParseCallStatement pins the bare `call "module.ann"` statement form: it
// parses to a *Call node carrying the verbatim path and its source line.
func TestParseCallStatement(t *testing.T) {
	prog := mustParse(t, "call \"mod.ann\"\n", PromptMode)
	if len(prog.Statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(prog.Statements))
	}
	c, ok := prog.Statements[0].(*Call)
	if !ok {
		t.Fatalf("statement 0 is %T, want *Call", prog.Statements[0])
	}
	if c.Path != "mod.ann" {
		t.Errorf("path = %q, want %q", c.Path, "mod.ann")
	}
	if c.Line != 1 {
		t.Errorf("line = %d, want 1", c.Line)
	}
}

// TestParseCallExpression pins the binding form `$x = call "module.ann"`: the
// assignment's right-hand side is a *Call expression.
func TestParseCallExpression(t *testing.T) {
	prog := mustParse(t, "$x = call \"mod.ann\"\n", PromptMode)
	as, ok := prog.Statements[0].(*Assign)
	if !ok {
		t.Fatalf("statement 0 is %T, want *Assign", prog.Statements[0])
	}
	if as.Name != "x" {
		t.Errorf("name = %q, want x", as.Name)
	}
	c, ok := as.Expr.(*Call)
	if !ok {
		t.Fatalf("expr is %T, want *Call", as.Expr)
	}
	if c.Path != "mod.ann" {
		t.Errorf("path = %q, want %q", c.Path, "mod.ann")
	}
}

// TestParseCallRequiresString proves a call keyword with no string path is a
// Syntax error reported at the keyword's line:column, both as a statement and
// as an expression.
func TestParseCallRequiresString(t *testing.T) {
	t.Run("bare_no_path", func(t *testing.T) {
		err := mustFail(t, "call\n", PromptMode, Syntax)
		if err.Line != 1 || err.Col != 1 {
			t.Errorf("error at %d:%d, want 1:1", err.Line, err.Col)
		}
	})
	t.Run("bare_non_string_path", func(t *testing.T) {
		mustFail(t, "call mod.ann\n", PromptMode, Syntax)
	})
	t.Run("expr_no_path", func(t *testing.T) {
		mustFail(t, "$x = call\n", PromptMode, Syntax)
	})
}

// TestParseCallFreeText proves `call` outside statement/expression head position
// stays ordinary text (R8): it is a plain dispatch arg and verbatim inside
// context, never triggering call parsing.
func TestParseCallFreeText(t *testing.T) {
	t.Run("dispatch_arg", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[echo] call now\n", PromptMode))
		if len(d.Args) != 2 || d.Args[0] != "call" || d.Args[1] != "now" {
			t.Errorf("args = %q, want [call now]", d.Args)
		}
	})
	t.Run("context_text", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[echo] : please call \"mod.ann\" later\n", PromptMode))
		if !strings.Contains(d.Context, "call") {
			t.Errorf("context = %q, want it to keep the word call", d.Context)
		}
	})
}
