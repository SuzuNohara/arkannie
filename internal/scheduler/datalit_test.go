package scheduler

import (
	"strings"
	"testing"

	"arkannie/internal/ann"
	"arkannie/internal/ram"
)

// newValState returns a scheduler and a fresh execState for exercising the
// value-construction helpers (listValue/concatValue) in isolation.
func newValState() (*Scheduler, *execState) {
	return &Scheduler{}, &execState{ram: ram.New()}
}

func strElem(s string) ann.Elem { return ann.Elem{Str: s} }
func refElem(p string) ann.Elem { return ann.Elem{IsRef: true, Str: p} }

func kstr(s string) ram.Value { return ram.Value{Kind: ram.KString, Str: s} }
func klist(v ...ram.Value) ram.Value {
	return ram.Value{Kind: ram.KList, List: v}
}

// TestListValueNested covers T2.1 value side: a nested list() element becomes a
// nested KList; scalars stay KString (§2.6, R6).
func TestListValueNested(t *testing.T) {
	s, st := newValState()
	l := ann.ListLit{Elems: []ann.Elem{
		strElem("a"),
		{List: &ann.ListLit{Elems: []ann.Elem{strElem("b"), strElem("c")}}},
	}}
	got := s.listValue(st, l)
	if got.Kind != ram.KList || len(got.List) != 2 {
		t.Fatalf("got %#v, want KList of 2", got)
	}
	if got.List[0].Kind != ram.KString || got.List[0].Str != "a" {
		t.Errorf("elem0 = %#v, want string a", got.List[0])
	}
	inner := got.List[1]
	if inner.Kind != ram.KList || len(inner.List) != 2 ||
		inner.List[0].Str != "b" || inner.List[1].Str != "c" {
		t.Errorf("elem1 = %#v, want nested list [b c]", inner)
	}
}

// TestConcatFlattenOneLevel covers T2.2: concat aplana exactamente UN nivel,
// orden estable a-luego-b; una lista anidada dentro de un arg permanece anidada.
func TestConcatFlattenOneLevel(t *testing.T) {
	s, st := newValState()
	_ = st.ram.Set("a", klist(kstr("a"), kstr("b")))
	_ = st.ram.Set("b", klist(kstr("c"), klist(kstr("d")))) // nested d stays nested
	got := s.concatValue(st, &ann.Concat{Args: []ann.Elem{refElem("a"), refElem("b")}})
	if got.Kind != ram.KList || len(got.List) != 4 {
		t.Fatalf("got %#v, want flat KList of 4", got)
	}
	if got.List[0].Str != "a" || got.List[1].Str != "b" || got.List[2].Str != "c" {
		t.Errorf("order wrong: %#v", got.List)
	}
	if got.List[3].Kind != ram.KList || len(got.List[3].List) != 1 || got.List[3].List[0].Str != "d" {
		t.Errorf("only one level flattened; elem3 should stay a nested list, got %#v", got.List[3])
	}
}

// TestConcatMixedNonList covers T2.3: non-list args become loose elements in
// their position.
func TestConcatMixedNonList(t *testing.T) {
	s, st := newValState()
	_ = st.ram.Set("lista", klist(kstr("a"), kstr("b")))
	_ = st.ram.Set("str", kstr("z"))
	got := s.concatValue(st, &ann.Concat{Args: []ann.Elem{refElem("lista"), strElem("mid"), refElem("str")}})
	want := []string{"a", "b", "mid", "z"}
	if len(got.List) != len(want) {
		t.Fatalf("len = %d, want %d (%#v)", len(got.List), len(want), got.List)
	}
	for i, w := range want {
		if got.List[i].Str != w {
			t.Errorf("elem[%d] = %q, want %q", i, got.List[i].Str, w)
		}
	}
}

// TestConcatBordersValue covers T2.5: concat() empty is a valid empty KList and
// concat($a) with one list arg flattens to a copy of it.
func TestConcatBordersValue(t *testing.T) {
	s, st := newValState()
	empty := s.concatValue(st, &ann.Concat{})
	if empty.Kind != ram.KList || len(empty.List) != 0 {
		t.Errorf("concat() = %#v, want empty KList", empty)
	}
	_ = st.ram.Set("a", klist(kstr("x"), kstr("y")))
	one := s.concatValue(st, &ann.Concat{Args: []ann.Elem{refElem("a")}})
	if len(one.List) != 2 || one.List[0].Str != "x" || one.List[1].Str != "y" {
		t.Errorf("concat($a) = %#v, want [x y]", one.List)
	}
}

// TestUnresolvableInListIsClassA covers T2.4: an unresolvable ref inside list()
// no longer yields a silent empty string; it emits a Class A notice and the
// element is OMITTED while the program continues (§8).
func TestUnresolvableInListIsClassA(t *testing.T) {
	s, st := newValState()
	got := s.listValue(st, ann.ListLit{Elems: []ann.Elem{refElem("missing"), strElem("ok")}})
	if len(got.List) != 1 || got.List[0].Str != "ok" {
		t.Fatalf("unresolvable element must be omitted, got %#v", got.List)
	}
	if len(s.Notices) != 1 || !strings.Contains(s.Notices[0], "[class A]") ||
		!strings.Contains(s.Notices[0], "missing") {
		t.Errorf("want one Class A notice naming 'missing', got %v", s.Notices)
	}
}

// TestUnresolvableInConcatIsClassA is the concat counterpart of T2.4.
func TestUnresolvableInConcatIsClassA(t *testing.T) {
	s, st := newValState()
	_ = st.ram.Set("a", klist(kstr("a")))
	got := s.concatValue(st, &ann.Concat{Args: []ann.Elem{refElem("a"), refElem("gone")}})
	if len(got.List) != 1 || got.List[0].Str != "a" {
		t.Fatalf("unresolvable concat arg must be omitted, got %#v", got.List)
	}
	if len(s.Notices) != 1 || !strings.Contains(s.Notices[0], "[class A]") ||
		!strings.Contains(s.Notices[0], "gone") {
		t.Errorf("want one Class A notice naming 'gone', got %v", s.Notices)
	}
}

// TestListValueMapElement covers the generic Map branch: an Elem carrying a
// MapLit evaluates to a KMap (map parsing lands in a later wave; the value path
// must already be generic).
func TestListValueMapElement(t *testing.T) {
	s, st := newValState()
	_ = st.ram.Set("v", kstr("resolved"))
	m := &ann.MapLit{Entries: []ann.MapEntry{
		{Key: "lit", Val: strElem("plain")},
		{Key: "ref", Val: refElem("v")},
	}}
	got := s.listValue(st, ann.ListLit{Elems: []ann.Elem{{Map: m}}})
	if len(got.List) != 1 || got.List[0].Kind != ram.KMap {
		t.Fatalf("want KList holding one KMap, got %#v", got)
	}
	mp := got.List[0].Map
	if mp["lit"].Str != "plain" || mp["ref"].Str != "resolved" {
		t.Errorf("map entries = %#v, want lit=plain ref=resolved", mp)
	}
}

// TestExecAssignConcat exercises the Concat case of execAssign end-to-end
// against RAM (§2.3, R5/R6).
func TestExecAssignConcat(t *testing.T) {
	s, st := newValState()
	_ = st.ram.Set("a", klist(kstr("a")))
	_ = st.ram.Set("b", klist(kstr("b")))
	as := &ann.Assign{Name: "out", Expr: &ann.Concat{Args: []ann.Elem{refElem("a"), refElem("b")}}}
	if esc := s.execAssign(st, as); esc != nil {
		t.Fatalf("execAssign escalated: %v", esc)
	}
	v, ok := st.ram.Resolve("out")
	if !ok || v.Kind != ram.KList || len(v.List) != 2 {
		t.Fatalf("binding out = %#v (ok=%v), want KList of 2", v, ok)
	}
}

// TestWalkRefsTracksListConcat covers R12 (F4): a binding referenced by a
// list()/concat() (including nested lists and maps) is tracked, so the
// checkpoint trigger sees the dependency and does not corrupt silently.
func TestWalkRefsTracksListConcat(t *testing.T) {
	dispatchAssign := func(name string) *ann.Assign {
		return &ann.Assign{Name: name, Expr: &ann.Dispatch{Command: "echo"}}
	}
	t.Run("concat_arg_tracked", func(t *testing.T) {
		stmts := []ann.Stmt{
			dispatchAssign("x"),
			&ann.Assign{Name: "l", Expr: &ann.Concat{Args: []ann.Elem{refElem("x"), strElem("y")}}},
		}
		if !producesReferencedBinding(stmts, 0) {
			t.Error("dispatch binding x referenced by concat must be checkpoint-tracked")
		}
	})
	t.Run("nested_list_ref_tracked", func(t *testing.T) {
		stmts := []ann.Stmt{
			dispatchAssign("x"),
			&ann.Assign{Name: "l", Expr: ann.ListLit{Elems: []ann.Elem{
				{List: &ann.ListLit{Elems: []ann.Elem{refElem("x")}}},
			}}},
		}
		if !producesReferencedBinding(stmts, 0) {
			t.Error("ref inside a nested list must be checkpoint-tracked")
		}
	})
	t.Run("map_value_ref_tracked", func(t *testing.T) {
		stmt := &ann.Assign{Name: "l", Expr: ann.ListLit{Elems: []ann.Elem{
			{Map: &ann.MapLit{Entries: []ann.MapEntry{{Key: "k", Val: refElem("z")}}}},
		}}}
		if !referencesBinding(stmt, "z") {
			t.Error("ref inside a map value must be discoverable by walkRefs")
		}
	})
	t.Run("unreferenced_not_tracked", func(t *testing.T) {
		stmts := []ann.Stmt{
			dispatchAssign("x"),
			&ann.Assign{Name: "l", Expr: ann.ListLit{Elems: []ann.Elem{strElem("lit")}}},
		}
		if producesReferencedBinding(stmts, 0) {
			t.Error("a list of literals must not register a phantom dependency")
		}
	})
}

// TestEscapedDollarInValues verifies R3 at the scheduler layer: `\$` in string
// literals and list elements yields a literal `$` without ref resolution.
func TestEscapedDollarInValues(t *testing.T) {
	t.Run("strlit_assign", func(t *testing.T) {
		v := ram.Unescape(`precio \$100`)
		if v != "precio $100" {
			t.Fatalf("Unescape strlit = %q, want %q", v, "precio $100")
		}
	})
	t.Run("list_element", func(t *testing.T) {
		s, st := newValState()
		got := s.listValue(st, ann.ListLit{Elems: []ann.Elem{strElem(`\$a`), strElem("b")}})
		if got.Kind != ram.KList || len(got.List) != 2 {
			t.Fatalf("got %#v, want KList of 2", got)
		}
		if got.List[0].Str != "$a" {
			t.Fatalf("first element = %q, want %q", got.List[0].Str, "$a")
		}
	})
}

// TestEscapedDollarInArgs covers the arg position at runtime (R3): a `\$` in a
// [return] string operand or keyword text reaches the output as a literal `$`.
func TestEscapedDollarInArgs(t *testing.T) {
	t.Run("return_string_operand", func(t *testing.T) {
		s, st := newValState()
		st.returnCounts = map[string]int{}
		s.execReturn(st, &ann.Dispatch{Command: "return", Args: []string{`cuesta \$5`}})
		out := st.report.String()
		if !strings.Contains(out, "cuesta $5") || strings.Contains(out, `\$`) {
			t.Fatalf("report = %q, want literal $5 without backslash", out)
		}
	})
	t.Run("notify_text", func(t *testing.T) {
		s, st := newValState()
		s.execKeyword(st, &ann.Dispatch{Command: "notify", Args: []string{`vale \$9`}})
		if len(s.Notices) != 1 || s.Notices[0] != "vale $9" {
			t.Fatalf("notices = %#v, want [vale $9]", s.Notices)
		}
	})
}

// TestAssignMapLit verifies a top-level `$m = map(...)` binds at runtime
// (execAssign wiring — C3-A gap closed by NOVA).
func TestAssignMapLit(t *testing.T) {
	s, st := newValState()
	err := s.execAssign(st, &ann.Assign{Name: "m", Expr: ann.MapLit{
		Entries: []ann.MapEntry{{Key: "k", Val: strElem("v")}},
	}})
	if err != nil {
		t.Fatalf("execAssign: %v", err)
	}
	v, ok := st.ram.Get("m")
	if !ok || v.Kind != ram.KMap || v.Map["k"].Str != "v" {
		t.Fatalf("binding m = %#v ok=%v, want KMap{k:v}", v, ok)
	}
}

// TestWalkRefsTracksTopLevelMap closes the wave-3 review finding: a binding
// referenced by a TOP-LEVEL map() assign must be checkpoint-tracked.
func TestWalkRefsTracksTopLevelMap(t *testing.T) {
	stmts := []ann.Stmt{
		&ann.Assign{Name: "x", Expr: &ann.Dispatch{Command: "echo"}},
		&ann.Assign{Name: "m", Expr: ann.MapLit{Entries: []ann.MapEntry{
			{Key: "k", Val: refElem("x")},
		}}},
	}
	if !producesReferencedBinding(stmts, 0) {
		t.Error("dispatch binding x referenced by top-level map() must be checkpoint-tracked")
	}
}
