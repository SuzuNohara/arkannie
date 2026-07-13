package scheduler

import (
	"strings"
	"testing"

	"arkannie/internal/ann"
)

// ifPermissive accepts any free-form success payload, so a dispatch can bind
// $r to a map with arbitrary fields (status, error) that the default single-
// field schema would reject.
const ifPermissive = "      success: {}\n"

// mapEnv is a success envelope whose payload is a map with the given fields
// rendered verbatim (each field on its own indented line, "key: value").
func mapEnv(id string, fields ...[2]string) string {
	out := "id: " + id + "\nstatus: success\npayload:\n"
	for _, f := range fields {
		out += "  " + f[0] + ": " + f[1] + "\n"
	}
	return out
}

func newIfScheduler(t *testing.T, stub *stubSpawner) *Scheduler {
	return newTestSchedulerSuccess(t, stub, ifPermissive, "echo")
}

// ranOnce reports whether the stub recorded exactly one attempt for id.
func ranOnce(stub *stubSpawner, id string) bool {
	return stub.calls[id] == 1
}

// TestExecIf pins the §8 conditional semantics (T-09/T-10): deterministic
// ==/!= comparison over resolved operands, null for irresolvable refs, per-
// branch scoping via Push/Pop, and a Class A skip of the whole statement when
// an operand is a composite (map/list) value.
func TestExecIf(t *testing.T) {
	t.Run("T2.1_eq_true_runs_only_then", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{mapEnv("a", [2]string{"status", "ok"})}
		s := newIfScheduler(t, stub)
		src := "$r = [echo] --id=a : \"x\"\n" +
			"if $r.status == \"ok\" {\n  [echo] --id=t : \"then\"\n}\nelse {\n  [echo] --id=e : \"else\"\n}\n"
		res := s.Run(parseProg(t, src), "if1", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if !ranOnce(stub, "t") {
			t.Errorf("then branch should run once; calls=%v", stub.calls)
		}
		if _, ran := stub.calls["e"]; ran {
			t.Errorf("else branch must not run; calls=%v", stub.calls)
		}
	})

	t.Run("T2.2_ne_selects_else_branch", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{mapEnv("a", [2]string{"status", "ok"})}
		s := newIfScheduler(t, stub)
		src := "$r = [echo] --id=a : \"x\"\n" +
			"if $r.status != \"ok\" {\n  [echo] --id=t : \"then\"\n}\nelse {\n  [echo] --id=e : \"else\"\n}\n"
		res := s.Run(parseProg(t, src), "if2", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if !ranOnce(stub, "e") {
			t.Errorf("else branch should run once; calls=%v", stub.calls)
		}
		if _, ran := stub.calls["t"]; ran {
			t.Errorf("then branch must not run; calls=%v", stub.calls)
		}
	})

	t.Run("T2.3_irresolvable_ref_is_null_null_eq_null_true", func(t *testing.T) {
		stub := newStub()
		s := newIfScheduler(t, stub)
		src := "if $missing == null {\n  [echo] --id=t : \"x\"\n}\n"
		res := s.Run(parseProg(t, src), "if3", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if !ranOnce(stub, "t") {
			t.Errorf("null==null must run then; calls=%v", stub.calls)
		}
	})

	t.Run("T2.3b_null_ne_string_is_false", func(t *testing.T) {
		stub := newStub()
		s := newIfScheduler(t, stub)
		src := "if $missing == \"ok\" {\n  [echo] --id=t : \"x\"\n}\nelse {\n  [echo] --id=e : \"y\"\n}\n"
		res := s.Run(parseProg(t, src), "if3b", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if _, ran := stub.calls["t"]; ran {
			t.Errorf("null==string must be false; then must not run; calls=%v", stub.calls)
		}
		if !ranOnce(stub, "e") {
			t.Errorf("else branch should run; calls=%v", stub.calls)
		}
	})

	t.Run("T2.4_presence_error_field_present", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{mapEnv("a", [2]string{"status", "ok"}, [2]string{"error", "boom"})}
		s := newIfScheduler(t, stub)
		src := "$r = [echo] --id=a : \"x\"\n" +
			"if $r.error != null {\n  [echo] --id=t : \"has-error\"\n}\nelse {\n  [echo] --id=e : \"clean\"\n}\n"
		res := s.Run(parseProg(t, src), "if4", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if !ranOnce(stub, "t") {
			t.Errorf("present error field != null should run then; calls=%v", stub.calls)
		}
		if _, ran := stub.calls["e"]; ran {
			t.Errorf("else branch must not run; calls=%v", stub.calls)
		}
	})

	t.Run("T2.4b_presence_error_field_absent", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{mapEnv("a", [2]string{"status", "ok"})}
		s := newIfScheduler(t, stub)
		src := "$r = [echo] --id=a : \"x\"\n" +
			"if $r.error != null {\n  [echo] --id=t : \"has-error\"\n}\nelse {\n  [echo] --id=e : \"clean\"\n}\n"
		res := s.Run(parseProg(t, src), "if4b", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if !ranOnce(stub, "e") {
			t.Errorf("absent error field resolves null; else should run; calls=%v", stub.calls)
		}
		if _, ran := stub.calls["t"]; ran {
			t.Errorf("then branch must not run; calls=%v", stub.calls)
		}
	})

	t.Run("T2.7_branch_scoping_and_dead_branch", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{mapEnv("a", [2]string{"status", "ok"})}
		s := newIfScheduler(t, stub)
		src := "$r = [echo] --id=a : \"x\"\n" +
			"if $r.status == \"ok\" {\n  $x = \"inside\"\n  [echo] --id=t : \"$x\"\n}\nelse {\n  [echo] --id=e : \"y\"\n}\n" +
			"[return] $x\n"
		res := s.Run(parseProg(t, src), "if7", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if !ranOnce(stub, "t") {
			t.Errorf("then branch should run once; calls=%v", stub.calls)
		}
		if _, ran := stub.calls["e"]; ran {
			t.Errorf("dead else branch must never dispatch; calls=%v", stub.calls)
		}
		// $x was visible inside the branch that created it.
		if len(stub.prompts["t"]) == 0 || !strings.Contains(stub.prompts["t"][0], "inside") {
			t.Errorf("branch-local $x not visible to its own dispatch; prompt=%v", stub.prompts["t"])
		}
		// ...and dies at branch exit: [return] $x after the if is unbound.
		if countContaining(s.Notices, "unbound") == 0 {
			t.Errorf("branch-local binding must not survive the if; notices=%v", s.Notices)
		}
	})

	t.Run("T2.6_composite_map_operand_skips_whole_statement", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{mapEnv("a", [2]string{"status", "ok"})}
		s := newIfScheduler(t, stub)
		src := "$r = [echo] --id=a : \"x\"\n" +
			"if $r == \"ok\" {\n  [echo] --id=t : \"then\"\n}\nelse {\n  [echo] --id=e : \"else\"\n}\n" +
			"[echo] --id=after : \"z\"\n"
		res := s.Run(parseProg(t, src), "if6", "")
		if res.Esc != nil {
			t.Fatalf("composite operand must not escalate: %s", res.Esc.Format())
		}
		if _, ran := stub.calls["t"]; ran {
			t.Errorf("no branch may run on composite skip; calls=%v", stub.calls)
		}
		if _, ran := stub.calls["e"]; ran {
			t.Errorf("no branch may run on composite skip; calls=%v", stub.calls)
		}
		if !ranOnce(stub, "after") {
			t.Errorf("program must continue past the skipped if; calls=%v", stub.calls)
		}
		if countContaining(s.Notices, "class A") == 0 {
			t.Errorf("composite operand must raise a Class A notice; notices=%v", s.Notices)
		}
	})

	t.Run("T2.6b_composite_list_operand_skips_whole_statement", func(t *testing.T) {
		stub := newStub()
		s := newIfScheduler(t, stub)
		src := "$l = list(\"a\", \"b\")\n" +
			"if $l == \"ok\" {\n  [echo] --id=t : \"then\"\n}\nelse {\n  [echo] --id=e : \"else\"\n}\n" +
			"[echo] --id=after : \"z\"\n"
		res := s.Run(parseProg(t, src), "if6b", "")
		if res.Esc != nil {
			t.Fatalf("composite list operand must not escalate: %s", res.Esc.Format())
		}
		if _, ran := stub.calls["t"]; ran {
			t.Errorf("no branch may run on composite skip; calls=%v", stub.calls)
		}
		if _, ran := stub.calls["e"]; ran {
			t.Errorf("no branch may run on composite skip; calls=%v", stub.calls)
		}
		if !ranOnce(stub, "after") {
			t.Errorf("program must continue past the skipped if; calls=%v", stub.calls)
		}
		if countContaining(s.Notices, "class A") == 0 {
			t.Errorf("composite list operand must raise a Class A notice; notices=%v", s.Notices)
		}
	})
}

// TestWalkRefsIf pins the checkpoint dependency tracking: walkRefs must visit
// both operands' refs (base name) and both branch bodies of an *ann.If.
func TestWalkRefsIf(t *testing.T) {
	ifStmt := &ann.If{
		Left:  ann.Operand{IsRef: true, Text: "a.status"},
		Op:    "==",
		Right: ann.Operand{IsRef: true, Text: "d"},
		Then:  []ann.Stmt{&ann.Dispatch{Command: "echo", Args: []string{"$b"}}},
		Else:  []ann.Stmt{&ann.Dispatch{Command: "echo", Args: []string{"$c"}}},
	}
	var got []string
	walkRefs(ifStmt, func(n string) { got = append(got, n) })
	for _, want := range []string{"a", "d", "b", "c"} {
		if !contains(got, want) {
			t.Errorf("walkRefs If missing ref %q; got=%v", want, got)
		}
	}
	// A string-literal operand contributes no ref.
	lit := &ann.If{Left: ann.Operand{IsRef: true, Text: "x"}, Op: "==", Right: ann.Operand{Text: "ok"}}
	got = nil
	walkRefs(lit, func(n string) { got = append(got, n) })
	if len(got) != 1 || got[0] != "x" {
		t.Errorf("literal operand should not add a ref; got=%v", got)
	}
}
