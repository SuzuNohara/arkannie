package scheduler

import "testing"

// TestExecLoopUntil pins the §8 loop post-condition semantics (T-13/T-14).
// The `until` guard is evaluated after each iteration body and BEFORE that
// iteration's RAM scope is popped, so it observes the bindings the body just
// created. A satisfied guard breaks the loop early; a guard that never holds
// runs exactly `limit` iterations; a composite operand is a Class A notice
// treated as unmet, so the loop runs to limit and the program continues.
//
// These reuse the same package test helpers as execif_test.go (newStub,
// newIfScheduler, mapEnv, parseProg, ranOnce, countContaining): the stub
// queues one response per --id and pops the next on each dispatch.
func TestExecLoopUntil(t *testing.T) {
	envFail := mapEnv("r", [2]string{"status", "failure"})
	envSucc := mapEnv("r", [2]string{"status", "success"})
	envOk := mapEnv("r", [2]string{"status", "ok"})

	t.Run("T3.1_retry_until_success_stops_at_third", func(t *testing.T) {
		stub := newStub()
		// Five responses queued; success arrives on the third. A correct until
		// stops there (3 dispatches). Ignoring until would consume all five.
		stub.byID["r"] = []string{envFail, envFail, envSucc, envSucc, envSucc}
		s := newIfScheduler(t, stub)
		src := "loop limit=5 until $r.status == \"success\" {\n" +
			"  $r = [echo] --id=r : \"x\"\n}\n"
		res := s.Run(parseProg(t, src), "loop1", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if stub.calls["r"] != 3 {
			t.Errorf("until success must stop at exactly iteration 3; calls=%v", stub.calls)
		}
	})

	t.Run("T3.2_never_true_runs_full_limit_then_continues", func(t *testing.T) {
		stub := newStub()
		stub.byID["r"] = []string{envFail, envFail, envFail}
		stub.byID["after"] = []string{mapEnv("after", [2]string{"status", "ok"})}
		s := newIfScheduler(t, stub)
		src := "loop limit=3 until $r.status == \"success\" {\n" +
			"  $r = [echo] --id=r : \"x\"\n}\n" +
			"[echo] --id=after : \"done\"\n"
		res := s.Run(parseProg(t, src), "loop2", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if stub.calls["r"] != 3 {
			t.Errorf("guard never true must run exactly limit iterations; calls=%v", stub.calls)
		}
		if !ranOnce(stub, "after") {
			t.Errorf("program must continue past the exhausted loop; calls=%v", stub.calls)
		}
	})

	t.Run("T3.3_guard_sees_body_bindings_anti_reorder", func(t *testing.T) {
		stub := newStub()
		// $r is assigned INSIDE the body. Evaluated before the iteration Pop, the
		// guard resolves $r.status="ok" on iteration 1 and stops (1 dispatch). If
		// the guard were evaluated after the Pop, $r would be unbound (null !=
		// "ok"), never hold, and the loop would run the full limit (5 dispatches).
		stub.byID["r"] = []string{envOk, envOk, envOk, envOk, envOk}
		s := newIfScheduler(t, stub)
		src := "loop limit=5 until $r.status == \"ok\" {\n" +
			"  $r = [echo] --id=r : \"x\"\n}\n"
		res := s.Run(parseProg(t, src), "loop3", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if stub.calls["r"] != 1 {
			t.Errorf("guard must see body-scoped $r and stop after 1 iteration; calls=%v", stub.calls)
		}
	})

	t.Run("T3.4_composite_operand_class_A_runs_to_limit", func(t *testing.T) {
		stub := newStub()
		// $r resolves to the whole payload map — a composite operand, not
		// comparable. The guard is a Class A notice, treated as unmet: the loop
		// runs to limit and the program continues past it (no escalation).
		stub.byID["r"] = []string{envFail, envFail, envFail}
		stub.byID["after"] = []string{mapEnv("after", [2]string{"status", "ok"})}
		s := newIfScheduler(t, stub)
		src := "loop limit=3 until $r == \"success\" {\n" +
			"  $r = [echo] --id=r : \"x\"\n}\n" +
			"[echo] --id=after : \"done\"\n"
		res := s.Run(parseProg(t, src), "loop4", "")
		if res.Esc != nil {
			t.Fatalf("composite until operand must not escalate: %s", res.Esc.Format())
		}
		if stub.calls["r"] != 3 {
			t.Errorf("composite guard treated as unmet must run to limit; calls=%v", stub.calls)
		}
		if !ranOnce(stub, "after") {
			t.Errorf("program must continue past the loop; calls=%v", stub.calls)
		}
		if countContaining(s.Notices, "class A") == 0 {
			t.Errorf("composite until operand must raise a Class A notice; notices=%v", s.Notices)
		}
	})

	t.Run("T3.5_no_until_runs_exactly_limit", func(t *testing.T) {
		stub := newStub()
		stub.byID["r"] = []string{envFail, envFail, envFail}
		s := newIfScheduler(t, stub)
		src := "loop limit=3 {\n  [echo] --id=r : \"x\"\n}\n"
		res := s.Run(parseProg(t, src), "loop5", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if stub.calls["r"] != 3 {
			t.Errorf("loop without until must iterate exactly limit times; calls=%v", stub.calls)
		}
	})
}
