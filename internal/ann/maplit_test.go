package ann

import (
	"strings"
	"testing"
)

// TestMapBasic covers T3.1: map(k: "v", n: $r.campo) parses to a MapLit with two
// entries in source order — a string-literal value and a dotted $ref value whose
// path is kept without the $ prefix (§2.6, v0.3, R7).
func TestMapBasic(t *testing.T) {
	ml, ok := assignExpr(t, "$m = map(k: \"v\", n: $r.campo)\n").(MapLit)
	if !ok {
		t.Fatalf("expr is not MapLit, got %T", assignExpr(t, "$m = map(k: \"v\", n: $r.campo)\n"))
	}
	if len(ml.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(ml.Entries))
	}
	if ml.Entries[0].Key != "k" || ml.Entries[0].Val.IsRef ||
		ml.Entries[0].Val.List != nil || ml.Entries[0].Val.Map != nil ||
		ml.Entries[0].Val.Str != "v" {
		t.Errorf("entry[0] = %#v, want k -> literal \"v\"", ml.Entries[0])
	}
	if ml.Entries[1].Key != "n" || !ml.Entries[1].Val.IsRef || ml.Entries[1].Val.Str != "r.campo" {
		t.Errorf("entry[1] = %#v, want n -> ref r.campo", ml.Entries[1])
	}
}

// TestMapNested covers T3.2: a map value may itself be a list() or a nested
// map(); the element grammar of list() is reused for values (§2.6, R7).
func TestMapNested(t *testing.T) {
	ml, ok := assignExpr(t, "$m = map(a: list(\"x\"), b: map(c: \"d\"))\n").(MapLit)
	if !ok {
		t.Fatalf("expr is not MapLit")
	}
	if len(ml.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(ml.Entries))
	}
	inner := ml.Entries[0].Val.List
	if inner == nil || len(inner.Elems) != 1 || inner.Elems[0].Str != "x" {
		t.Errorf("entry a = %#v, want list(\"x\")", ml.Entries[0].Val)
	}
	nested := ml.Entries[1].Val.Map
	if nested == nil || len(nested.Entries) != 1 ||
		nested.Entries[0].Key != "c" || nested.Entries[0].Val.Str != "d" {
		t.Errorf("entry b = %#v, want map(c: \"d\")", ml.Entries[1].Val)
	}
}

// TestMapDuplicateKey covers T3.3: a duplicate key is a Syntax error carrying
// the offending line:column, and it names the duplicated key (§7.1).
func TestMapDuplicateKey(t *testing.T) {
	err := mustFail(t, "$m = map(k: \"a\", k: \"b\")\n", PromptMode, Syntax)
	if !strings.Contains(err.Msg, "duplicate") || !strings.Contains(err.Msg, "k") {
		t.Errorf("msg = %q, want a duplicate-key message naming k", err.Msg)
	}
	if err.Line != 1 || err.Col < 1 {
		t.Errorf("position = %d:%d, want a valid 1-based L:C on line 1", err.Line, err.Col)
	}
}

// TestMapSyntaxErrors covers T3.4: malformed map() forms are Syntax errors with
// a map-specific message and a valid line:column.
func TestMapSyntaxErrors(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"missing_colon", "$m = map(k \"v\")\n", "map key"},
		{"unclosed", "$m = map(k: \"v\"\n", "unclosed"},
		{"non_ident_key", "$m = map(\"k\": \"v\")\n", "map key"},
		{"empty_value", "$m = map(k: )\n", "map key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mustFail(t, tc.src, PromptMode, Syntax)
			if !strings.Contains(err.Msg, tc.want) {
				t.Errorf("msg = %q, want substring %q", err.Msg, tc.want)
			}
			if err.Line != 1 || err.Col < 1 {
				t.Errorf("position = %d:%d, want a valid 1-based L:C on line 1", err.Line, err.Col)
			}
		})
	}
}

// TestMapAsElement covers T3.4/§2.6: map() may appear as an element inside
// list() and concat(), landing in Elem.Map so the value walker stays generic.
func TestMapAsElement(t *testing.T) {
	t.Run("inside_list", func(t *testing.T) {
		ll := assignExpr(t, "$l = list(map(k: \"v\"), \"tail\")\n").(ListLit)
		if len(ll.Elems) != 2 || ll.Elems[0].Map == nil {
			t.Fatalf("elems = %#v, want [map(...) \"tail\"]", ll.Elems)
		}
		if ll.Elems[0].Map.Entries[0].Key != "k" || ll.Elems[0].Map.Entries[0].Val.Str != "v" {
			t.Errorf("nested map = %#v, want k -> v", ll.Elems[0].Map)
		}
	})
	t.Run("inside_concat", func(t *testing.T) {
		c := assignExpr(t, "$l = concat(map(a: $x), $rest)\n").(*Concat)
		if len(c.Args) != 2 || c.Args[0].Map == nil {
			t.Fatalf("args = %#v, want [map(...) $rest]", c.Args)
		}
		if !c.Args[0].Map.Entries[0].Val.IsRef || c.Args[0].Map.Entries[0].Val.Str != "x" {
			t.Errorf("nested map value = %#v, want ref x", c.Args[0].Map.Entries[0].Val)
		}
	})
}

// TestMapAsTextPositional covers T3.6/R8: `map` is only a constructor when
// immediately followed by '(' in expression position; as a bare positional arg
// (or amidst other words) it stays ordinary text and never breaks the parse.
func TestMapAsTextPositional(t *testing.T) {
	d := firstDispatch(t, mustParse(t, "[echo] use map for config\n", PromptMode))
	want := []string{"use", "map", "for", "config"}
	if len(d.Args) != len(want) {
		t.Fatalf("args = %q, want %q", d.Args, want)
	}
	for i, w := range want {
		if d.Args[i] != w {
			t.Errorf("arg[%d] = %q, want %q", i, d.Args[i], w)
		}
	}
}
