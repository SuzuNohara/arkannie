package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"arkannie/internal/spawn"
)

// ---------------------------------------------------------------------------
// TestFanout — the dynamic parallel foreach fan-out (R9, R10, R11, R17).
// ---------------------------------------------------------------------------

// TestFanoutThreeSpawns (T4.1): a list of 3 fans out to synthetic ids W-1..W-3
// and the each handler runs once per item.
func TestFanoutThreeSpawns(t *testing.T) {
	stub := newStub()
	s := newTestScheduler(t, stub, "echo")
	src := "$items = list(\"a\", \"b\", \"c\")\n" +
		"parallel foreach $items --id=W {\n  [echo] : $item\n}\n" +
		"  each -> {\n    [notify] : each-ran\n  }\n"
	res := s.Run(parseProg(t, src), "f1", "")
	if res.Esc != nil {
		t.Fatalf("unexpected escalation: %s", res.Esc.Format())
	}
	for _, id := range []string{"W-1", "W-2", "W-3"} {
		if stub.calls[id] != 1 {
			t.Errorf("id %s spawned %d times, want 1; calls=%v", id, stub.calls[id], stub.calls)
		}
	}
	if got := countContaining(s.Notices, "each-ran"); got != 3 {
		t.Errorf("each handler ran %d times, want 3; notices=%v", got, s.Notices)
	}
}

// reverseStub forces a deterministic REVERSE completion order (W-3, then W-2,
// then W-1) by chaining each id on the previous one's done channel — no sleeps.
type reverseStub struct {
	mu      sync.Mutex
	order   []string
	waitFor map[string]chan struct{}
	doneCh  map[string]chan struct{}
}

func (rs *reverseStub) Run(_ context.Context, spec spawn.RunSpec) (spawn.Result, error) {
	prompt, _ := os.ReadFile(spec.PromptFile)
	id := ""
	if m := idRe.FindSubmatch(prompt); m != nil {
		id = string(m[1])
	}
	if ch := rs.waitFor[id]; ch != nil {
		<-ch
	}
	rs.mu.Lock()
	rs.order = append(rs.order, id)
	rs.mu.Unlock()
	if ch := rs.doneCh[id]; ch != nil {
		close(ch)
	}
	payload := map[string]string{"W-1": "one", "W-2": "two", "W-3": "three"}[id]
	out, _ := json.Marshal(map[string]string{"result": okEnv(id, "out", payload)})
	return spawn.Result{Stdout: out}, nil
}

// TestFanoutDeterministicReport (T4.2): with completion arriving in reverse order
// the each report is still assembled strictly in index order (W-1, W-2, W-3).
func TestFanoutDeterministicReport(t *testing.T) {
	rs := &reverseStub{
		waitFor: map[string]chan struct{}{},
		doneCh:  map[string]chan struct{}{"W-1": make(chan struct{}), "W-2": make(chan struct{}), "W-3": make(chan struct{})},
	}
	rs.waitFor["W-2"] = rs.doneCh["W-3"] // W-2 completes after W-3
	rs.waitFor["W-1"] = rs.doneCh["W-2"] // W-1 completes after W-2

	s := newTestScheduler(t, rs, "echo")
	src := "$items = list(\"a\", \"b\", \"c\")\n" +
		"parallel foreach $items --id=W {\n  [echo] : $item\n}\n" +
		"  each -> {\n    [return] --id=step $result.payload.out\n  }\n"
	res := s.Run(parseProg(t, src), "f2", "")
	if res.Esc != nil {
		t.Fatalf("unexpected escalation: %s", res.Esc.Format())
	}
	if len(rs.order) != 3 || rs.order[0] != "W-3" || rs.order[2] != "W-1" {
		t.Fatalf("completion order = %v, want reverse [W-3 W-2 W-1]", rs.order)
	}
	iOne := strings.Index(res.Report, "one")
	iTwo := strings.Index(res.Report, "two")
	iThree := strings.Index(res.Report, "three")
	if iOne < 0 || iTwo < 0 || iThree < 0 || !(iOne < iTwo && iTwo < iThree) {
		t.Errorf("report not in index order (one<two<three); report:\n%s", res.Report)
	}
	if !strings.Contains(res.Report, "## step-1") || !strings.Contains(res.Report, "## step-3") {
		t.Errorf("each returns not numbered per index; report:\n%s", res.Report)
	}
}

// TestFanoutNonList (T4.3): a non-list binding is a Class A notice + skip, and
// the program continues past the statement.
func TestFanoutNonList(t *testing.T) {
	stub := newStub()
	s := newTestScheduler(t, stub, "echo")
	src := "$x = \"scalar\"\n" +
		"parallel foreach $x --id=W {\n  [echo] : $item\n}\n" +
		"[echo] --id=after : \"z\"\n"
	res := s.Run(parseProg(t, src), "f3", "")
	if res.Esc != nil {
		t.Fatalf("non-list fan-out must be Class A, got: %s", res.Esc.Format())
	}
	if countContaining(s.Notices, "not a list") == 0 {
		t.Errorf("expected a 'not a list' notice; notices=%v", s.Notices)
	}
	if _, ran := stub.calls["W-1"]; ran {
		t.Errorf("fan-out over non-list must not spawn; calls=%v", stub.calls)
	}
	if stub.calls["after"] != 1 {
		t.Errorf("program should continue past Class A; calls=%v", stub.calls)
	}
}

// TestFanoutEmptyList (T4.4): an empty list fans out to zero spawns, the each
// never runs, and there is no error.
func TestFanoutEmptyList(t *testing.T) {
	stub := newStub()
	s := newTestScheduler(t, stub, "echo")
	src := "$items = list()\n" +
		"parallel foreach $items --id=W {\n  [echo] : $item\n}\n" +
		"  each -> {\n    [notify] : each-ran\n  }\n"
	res := s.Run(parseProg(t, src), "f4", "")
	if res.Esc != nil {
		t.Fatalf("unexpected escalation: %s", res.Esc.Format())
	}
	if stub.total() != 0 {
		t.Errorf("empty fan-out spawned %d, want 0", stub.total())
	}
	if countContaining(s.Notices, "each-ran") != 0 {
		t.Errorf("each must not run on an empty list; notices=%v", s.Notices)
	}
}

// TestFanoutSemaphoreBound (T4.5): a list larger than MaxConcurrency completes
// fully under the semaphore.
func TestFanoutSemaphoreBound(t *testing.T) {
	stub := newStub()
	s := newTestScheduler(t, stub, "echo")
	s.Cfg.MaxConcurrency = 2
	src := "$items = list(\"a\", \"b\", \"c\", \"d\")\n" +
		"parallel foreach $items --id=W {\n  [echo] : $item\n}\n  each -> {}\n"
	res := s.Run(parseProg(t, src), "f5", "")
	if res.Esc != nil {
		t.Fatalf("unexpected escalation: %s", res.Esc.Format())
	}
	if stub.total() != 4 {
		t.Errorf("fan-out of 4 under MaxConcurrency=2 ran %d, want 4", stub.total())
	}
}

// TestFanoutItemScopeGone (T4.6): $item / $index render into the template per
// item but do not survive past the statement.
func TestFanoutItemScopeGone(t *testing.T) {
	stub := newStub()
	s := newTestScheduler(t, stub, "echo")
	src := "$items = list(\"a\", \"b\")\n" +
		"parallel foreach $items --id=W {\n  [echo] : idx=$index val=$item\n}\n" +
		"[return] $item\n"
	res := s.Run(parseProg(t, src), "f6", "")
	if res.Esc != nil {
		t.Fatalf("unexpected escalation: %s", res.Esc.Format())
	}
	if len(stub.prompts["W-1"]) == 0 || !strings.Contains(stub.prompts["W-1"][0], "idx=1 val=a") {
		t.Errorf("W-1 template missing item/index binding; prompts=%v", stub.prompts["W-1"])
	}
	if len(stub.prompts["W-2"]) == 0 || !strings.Contains(stub.prompts["W-2"][0], "idx=2 val=b") {
		t.Errorf("W-2 template missing item/index binding; prompts=%v", stub.prompts["W-2"])
	}
	if countContaining(s.Notices, "unbound") == 0 {
		t.Errorf("$item must be unbound after the statement; notices=%v", s.Notices)
	}
}

// TestFanoutErrorWithoutEach (T4.7): an item error with no each handler escalates
// normally after all items complete.
func TestFanoutErrorWithoutEach(t *testing.T) {
	stub := newStub()
	stub.byID["W-2"] = []string{errEnv("W-2", "boom")}
	s := newTestScheduler(t, stub, "echo")
	src := "$items = list(\"a\", \"b\", \"c\")\n" +
		"parallel foreach $items --id=W {\n  [echo] : $item\n}\n"
	res := s.Run(parseProg(t, src), "f7", "")
	if res.Esc == nil || res.Esc.Class != 'B' {
		t.Fatalf("want Class B escalation, got %+v", res.Esc)
	}
	if res.Esc.Title != "unhandled parallel errors" {
		t.Errorf("title = %q, want unhandled parallel errors", res.Esc.Title)
	}
	for _, id := range []string{"W-1", "W-2", "W-3"} {
		if stub.calls[id] != 1 {
			t.Errorf("all items must complete before the ordered cut; id %s calls=%d", id, stub.calls[id])
		}
	}
}

// TestFanoutUnknownCommand: an unknown template command is a Class B escalation
// during preparation (mirrors a bare dispatch).
func TestFanoutUnknownCommand(t *testing.T) {
	stub := newStub()
	s := newTestScheduler(t, stub, "echo")
	src := "$items = list(\"a\")\n" +
		"parallel foreach $items --id=W {\n  [nope] : $item\n}\n"
	res := s.Run(parseProg(t, src), "f8", "")
	if res.Esc == nil || res.Esc.Class != 'B' || res.Esc.Title != "unknown command" {
		t.Fatalf("want Class B unknown command, got %+v", res.Esc)
	}
}

// TestFanoutWalkRefs (T4.10/R17): walkRefs tracks the list ref plus every ref in
// the template and the each handler, so checkpoint dependency tracking is intact.
func TestFanoutWalkRefs(t *testing.T) {
	src := "parallel foreach $items --id=W {\n  [echo] : \"$foo $item\"\n}\n" +
		"  each -> {\n    [echo] : \"$bar\"\n  }\n"
	prog := parseProg(t, src)
	found := map[string]bool{}
	walkRefs(prog.Statements[0], func(n string) { found[n] = true })
	for _, want := range []string{"items", "foo", "bar"} {
		if !found[want] {
			t.Errorf("walkRefs missing %q; found=%v", want, found)
		}
	}
}
