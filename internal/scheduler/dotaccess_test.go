package scheduler

import (
	"strings"
	"testing"

	"arkannie/internal/ann"
)

// permissiveSuccess is a success schema that accepts any free-form payload, so
// tests can bind rich maps/lists without tripping strict schema matching.
const permissiveSuccess = "      success: {}\n"

// TestDotAccess exercises dot-access end-to-end through the scheduler at the
// only position the Ann lexer preserves a dotted reference: the verbatim
// context text after `:` (§9 interpolation). A $x.field path interpolates the
// VALUE of the field, deep paths walk nested maps, and an unresolvable path in
// interpolation position escalates Class B naming the base and the failing
// segment. A plain $x is unchanged (T-05, R2/R3).
func TestDotAccess(t *testing.T) {
	t.Run("T1.1_context_interpolates_field_value_not_full_map", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{"id: a\nstatus: success\npayload:\n  name: suzu\n  team: core\n"}
		s := newTestSchedulerSuccess(t, stub, permissiveSuccess, "echo")
		res := s.Run(parseProg(t, "$rec = [echo] --id=a : \"x\"\n[echo] --id=b : $rec.name\n"), "d1", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		p := stub.prompts["b"]
		if len(p) == 0 {
			t.Fatal("second dispatch never invoked")
		}
		if !strings.Contains(p[0], "text: suzu") {
			t.Errorf("context must inline the field value 'suzu'; prompt:\n%s", p[0])
		}
		if strings.Contains(p[0], "team: core") {
			t.Errorf("context must not dump the full map; prompt:\n%s", p[0])
		}
		if strings.Contains(p[0], "rec.name") {
			t.Errorf("dotted token must be resolved, not left literal; prompt:\n%s", p[0])
		}
	})

	t.Run("T1.2_deep_path_in_context", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{"id: a\nstatus: success\npayload:\n  addr:\n    city: Springfield\n"}
		s := newTestSchedulerSuccess(t, stub, permissiveSuccess, "echo")
		res := s.Run(parseProg(t, "$rec = [echo] --id=a : \"x\"\n[echo] --id=b : in $rec.addr.city today\n"), "d2", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		p := stub.prompts["b"]
		if len(p) == 0 || !strings.Contains(p[0], "text: in Springfield today") {
			t.Errorf("deep dotted path must resolve to 'Springfield'; prompts=%v", p)
		}
	})

	t.Run("T1.6_missing_field_in_interpolation_is_classB", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{"id: a\nstatus: success\npayload:\n  name: suzu\n"}
		s := newTestSchedulerSuccess(t, stub, permissiveSuccess, "echo")
		res := s.Run(parseProg(t, "$rec = [echo] --id=a : \"x\"\n[echo] --id=b : $rec.missing\n"), "d4", "")
		if res.Esc == nil || res.Esc.Class != 'B' {
			t.Fatalf("want Class B escalation, got %+v", res.Esc)
		}
		if !strings.Contains(res.Esc.Detail, "rec") || !strings.Contains(res.Esc.Detail, "missing") {
			t.Errorf("detail must name base 'rec' and segment 'missing': %q", res.Esc.Detail)
		}
	})

	t.Run("T1.7_non_map_traversal_is_classB_with_cut_suggestion", func(t *testing.T) {
		s := newTestScheduler(t, newStub(), "echo")
		res := s.Run(parseProg(t, "$v = \"lit\"\n[echo] --id=b : $v.2\n"), "d5", "")
		if res.Esc == nil || res.Esc.Class != 'B' {
			t.Fatalf("want Class B escalation, got %+v", res.Esc)
		}
		if !strings.Contains(res.Esc.Detail, "separate") {
			t.Errorf("detail must suggest separating the dot from the reference: %q", res.Esc.Detail)
		}
	})

	t.Run("T1.8_plain_ref_regression", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{okEnv("a", "out", "hello")}
		s := newTestScheduler(t, stub, "echo")
		res := s.Run(parseProg(t, "$x = [echo] --id=a : \"x\"\n[echo] --id=b : $x\n"), "d6", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		p := stub.prompts["b"]
		if len(p) == 0 || !strings.Contains(p[0], "out: hello") {
			t.Errorf("plain $x must resolve exactly as before; prompts=%v", p)
		}
	})
}

// TestDotAccessResolveWiring pins that Resolve is cabled at the [return],
// list() and foreach value sites. The Ann grammar now preserves dotted
// references at these positions (see TestAnnParserAcceptsDottedRefs); these
// assertions remain the regression guard that Resolve behaves exactly like Get
// for the undotted case, while TestDottedRefsEndToEnd covers the dotted case.
func TestDotAccessResolveWiring(t *testing.T) {
	t.Run("return_resolves_whole_binding", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{okEnv("a", "out", "hello")}
		s := newTestScheduler(t, stub, "echo")
		res := s.Run(parseProg(t, "$r = [echo] --id=a : \"x\"\n[return] --id=res $r\n"), "w1", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if !strings.Contains(res.Report, "## res") || !strings.Contains(res.Report, "out: hello") {
			t.Errorf("[return] $r must render the binding; report:\n%s", res.Report)
		}
	})

	t.Run("foreach_and_list_resolve_plain_refs", func(t *testing.T) {
		stub := newStub()
		s := newTestScheduler(t, stub, "echo")
		res := s.Run(parseProg(t, "$x = \"lit\"\n$l = list($x, \"b\")\nforeach $l {\n  [echo] : $item\n}\n"), "w2", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if stub.total() != 2 {
			t.Errorf("foreach over 2-element list built by list($x,..) ran %d, want 2", stub.total())
		}
	})
}

// TestAnnParserAcceptsDottedRefs is the inverse of the former gate test: the
// Ann grammar now keeps a dotted reference as a single token/path in argument
// (including [return]) and foreach-list position, so these sites receive the
// full path and can be wired end-to-end.
func TestAnnParserAcceptsDottedRefs(t *testing.T) {
	prog, err := ann.Parse([]byte("[return] --id=r $result.payload\n"), ann.PromptMode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, ok := prog.Statements[0].(*ann.Dispatch)
	if !ok {
		t.Fatalf("want *ann.Dispatch, got %T", prog.Statements[0])
	}
	// The dot is now part of the reference: a single arg holds the whole path.
	if len(d.Args) != 1 || d.Args[0] != "$result.payload" {
		t.Errorf("expected dotted arg to stay one ref [$result.payload], got %#v", d.Args)
	}

	fprog, ferr := ann.Parse([]byte("$r = [echo] --id=a : \"x\"\nforeach $r.items {\n  [echo] : $item\n}\n"), ann.PromptMode)
	if ferr != nil {
		t.Fatalf("dotted foreach list must parse, got error: %v", ferr)
	}
	fe, ok := fprog.Statements[1].(*ann.Foreach)
	if !ok {
		t.Fatalf("statement 1 is %T, want *ann.Foreach", fprog.Statements[1])
	}
	if fe.List != "r.items" {
		t.Errorf("foreach list = %q, want r.items", fe.List)
	}
}

// TestDottedRefsEndToEnd exercises dot-access through the scheduler at the arg
// ([return]) and foreach-list sites now that the grammar preserves the path
// (R2, T1.3/T1.4/T1.5).
func TestDottedRefsEndToEnd(t *testing.T) {
	t.Run("T1.5_return_dotted_field_renders_field_value", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{"id: a\nstatus: success\npayload:\n  campo: found\n  other: nope\n"}
		s := newTestSchedulerSuccess(t, stub, permissiveSuccess, "echo")
		res := s.Run(parseProg(t, "$x = [echo] --id=a : \"s\"\n[return] --id=res $x.campo\n"), "d7", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if !strings.Contains(res.Report, "## res") || !strings.Contains(res.Report, "found") {
			t.Errorf("[return] $x.campo must render the field value 'found'; report:\n%s", res.Report)
		}
		if strings.Contains(res.Report, "nope") {
			t.Errorf("[return] $x.campo must not dump sibling fields; report:\n%s", res.Report)
		}
	})

	t.Run("T1.4_foreach_over_dotted_list_iterates_each_item", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{"id: a\nstatus: success\npayload:\n  items:\n    - alpha\n    - beta\n"}
		s := newTestSchedulerSuccess(t, stub, permissiveSuccess, "echo")
		res := s.Run(parseProg(t, "$r = [echo] --id=a : \"s\"\nforeach $r.items {\n  [echo] : $item\n}\n"), "d8", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if got := stub.total(); got != 3 {
			t.Errorf("seed + 2 foreach iterations = 3 dispatches, got %d (list may not have resolved)", got)
		}
		for _, n := range s.Notices {
			if strings.Contains(n, "not a list") {
				t.Errorf("dotted foreach list resolved to non-list: %q", n)
			}
		}
		var itemPrompts []string
		for _, ps := range stub.prompts {
			itemPrompts = append(itemPrompts, ps...)
		}
		if countContaining(itemPrompts, "text: alpha") == 0 || countContaining(itemPrompts, "text: beta") == 0 {
			t.Errorf("foreach body must bind $item to each list element; prompts=%v", itemPrompts)
		}
	})
}
