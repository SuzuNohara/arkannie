package ann

import (
	"strings"
	"testing"
)

// elemSrc reconstructs the source token form of a parsed list/concat element.
// It is the migration bridge for assertions written against the pre-v0.3
// `Elems []string` shape (§2.6).
func elemSrc(e Elem) string {
	switch {
	case e.IsRef:
		return "$" + e.Str
	case e.List != nil:
		parts := make([]string, len(e.List.Elems))
		for i, el := range e.List.Elems {
			parts[i] = elemSrc(el)
		}
		return "list(" + strings.Join(parts, ", ") + ")"
	default:
		return e.Str
	}
}

func assignExpr(t *testing.T, src string) Expr {
	t.Helper()
	prog := mustParse(t, src, PromptMode)
	as, ok := prog.Statements[0].(*Assign)
	if !ok {
		t.Fatalf("statement 0 is %T, want *Assign", prog.Statements[0])
	}
	return as.Expr
}

// TestListNested covers T2.1: list() may hold a nested list() as one element,
// producing a ListLit whose element carries the inner ListLit (§2.6, R5).
func TestListNested(t *testing.T) {
	ll, ok := assignExpr(t, "$l = list(\"a\", list(\"b\", \"c\"))\n").(ListLit)
	if !ok {
		t.Fatalf("expr is not ListLit")
	}
	if len(ll.Elems) != 2 {
		t.Fatalf("outer elems = %d, want 2", len(ll.Elems))
	}
	if ll.Elems[0].IsRef || ll.Elems[0].List != nil || ll.Elems[0].Str != "a" {
		t.Errorf("elem[0] = %#v, want literal \"a\"", ll.Elems[0])
	}
	inner := ll.Elems[1].List
	if inner == nil {
		t.Fatalf("elem[1] must carry a nested list, got %#v", ll.Elems[1])
	}
	if len(inner.Elems) != 2 || inner.Elems[0].Str != "b" || inner.Elems[1].Str != "c" {
		t.Errorf("inner list = %#v, want [b c]", inner.Elems)
	}
}

// TestListDottedElement is the v0.2 regression (T2.6): dotted refs stay a single
// element with the path (without $) in Str and IsRef set.
func TestListDottedElement(t *testing.T) {
	ll := assignExpr(t, "$l = list($x.a, \"b\", $y.p.q)\n").(ListLit)
	want := []string{"$x.a", "b", "$y.p.q"}
	if len(ll.Elems) != len(want) {
		t.Fatalf("elems = %d, want %d", len(ll.Elems), len(want))
	}
	for i, w := range want {
		if elemSrc(ll.Elems[i]) != w {
			t.Errorf("elem[%d] = %q, want %q", i, elemSrc(ll.Elems[i]), w)
		}
	}
	if !ll.Elems[0].IsRef || ll.Elems[0].Str != "x.a" {
		t.Errorf("elem[0] = %#v, want ref path x.a", ll.Elems[0])
	}
}

// TestConcatBasic covers T2.2 parse side: concat($a, $b) is a *Concat with two
// ref args in stable order (R5).
func TestConcatBasic(t *testing.T) {
	c, ok := assignExpr(t, "$l = concat($a, $b)\n").(*Concat)
	if !ok {
		t.Fatalf("expr is not *Concat")
	}
	if len(c.Args) != 2 {
		t.Fatalf("args = %d, want 2", len(c.Args))
	}
	if !c.Args[0].IsRef || c.Args[0].Str != "a" || !c.Args[1].IsRef || c.Args[1].Str != "b" {
		t.Errorf("args = %#v, want refs a,b in order", c.Args)
	}
}

// TestConcatMixed covers T2.3 parse side: non-list args parse as ordinary
// elements in their position.
func TestConcatMixed(t *testing.T) {
	c := assignExpr(t, "$l = concat($lista, \"x\", $str)\n").(*Concat)
	want := []string{"$lista", "x", "$str"}
	if len(c.Args) != len(want) {
		t.Fatalf("args = %d, want %d", len(c.Args), len(want))
	}
	for i, w := range want {
		if elemSrc(c.Args[i]) != w {
			t.Errorf("arg[%d] = %q, want %q", i, elemSrc(c.Args[i]), w)
		}
	}
}

// TestConcatBorders covers T2.5 parse side: concat() with no args and concat
// with a single arg both parse.
func TestConcatBorders(t *testing.T) {
	empty := assignExpr(t, "$l = concat()\n").(*Concat)
	if len(empty.Args) != 0 {
		t.Errorf("concat() args = %d, want 0", len(empty.Args))
	}
	one := assignExpr(t, "$l = concat($a)\n").(*Concat)
	if len(one.Args) != 1 || !one.Args[0].IsRef || one.Args[0].Str != "a" {
		t.Errorf("concat($a) args = %#v, want single ref a", one.Args)
	}
}

// TestConcatNestedListArg: a concat arg may itself be a list() literal.
func TestConcatNestedListArg(t *testing.T) {
	c := assignExpr(t, "$l = concat(list(\"a\", \"b\"), $c)\n").(*Concat)
	if len(c.Args) != 2 {
		t.Fatalf("args = %d, want 2", len(c.Args))
	}
	if c.Args[0].List == nil || len(c.Args[0].List.Elems) != 2 {
		t.Errorf("arg[0] must be a nested list of 2, got %#v", c.Args[0])
	}
}

// TestKeywordsAsText covers R8: `concat` and `map` are only special immediately
// before `(` in expression position; as bare words (args), inside context text,
// and inside string literals they stay verbatim and never break the parse.
func TestKeywordsAsText(t *testing.T) {
	t.Run("bare_words_as_args", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[echo] concat map\n", PromptMode))
		if len(d.Args) != 2 || d.Args[0] != "concat" || d.Args[1] != "map" {
			t.Errorf("args = %q, want [concat map]", d.Args)
		}
	})
	t.Run("in_context_text", func(t *testing.T) {
		d := firstDispatch(t, mustParse(t, "[echo] : call concat(a) or map(b) here\n", PromptMode))
		if d.Context != "call concat(a) or map(b) here" {
			t.Errorf("context = %q, want verbatim with concat(/map(", d.Context)
		}
	})
	t.Run("in_string_literal", func(t *testing.T) {
		lit, ok := assignExpr(t, "$s = \"concat(a, b) and map(c)\"\n").(StrLit)
		if !ok || lit.Value != "concat(a, b) and map(c)" {
			t.Errorf("string literal = %#v, want verbatim", assignExpr(t, "$s = \"concat(a, b) and map(c)\"\n"))
		}
	})
}

// TestListConcatErrorsLC covers L:C error reporting on malformed constructors.
func TestListConcatErrorsLC(t *testing.T) {
	t.Run("unclosed_concat", func(t *testing.T) {
		err := mustFail(t, "$l = concat($a, $b\n", PromptMode, Syntax)
		if err.Line != 1 {
			t.Errorf("line = %d, want 1 (err %v)", err.Line, err)
		}
	})
	t.Run("bad_element", func(t *testing.T) {
		mustFail(t, "$l = list(==)\n", PromptMode, Syntax)
	})
	t.Run("trailing_after_concat", func(t *testing.T) {
		mustFail(t, "$l = concat($a) extra\n", PromptMode, Syntax)
	})
}
