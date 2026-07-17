package scheduler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arkannie/internal/checkpoint"
	"arkannie/internal/config"
	"arkannie/internal/registry"
	"arkannie/internal/spawn"
)

// newCallScheduler builds a scheduler factory sharing one root (program dir),
// memDir (checkpoint + run-dir store) and registry, so a parent program run with
// a programPath under root can `call` module files written into root.
func newCallScheduler(t *testing.T) (mk func(spawn.Spawner) *Scheduler, root, memDir string) {
	t.Helper()
	root = t.TempDir()
	agentsDir := filepath.Join(root, ".agents")
	writeAgent(t, agentsDir, "echo")
	reg, errs := registry.Load(agentsDir)
	if len(errs) > 0 {
		t.Fatalf("registry load: %v", errs)
	}
	cfg := &config.Config{TimeoutDefault: 60, MaxConcurrency: 4, ClaudeBin: "claude", Root: root}
	memDir = filepath.Join(root, ".mem")
	mk = func(sp spawn.Spawner) *Scheduler {
		return New(reg, cfg, sp, memDir, filepath.Join(agentsDir, ".personalities"), root, spawn.Consent{})
	}
	return mk, root, memDir
}

// writeModule writes a called .ann module into dir and returns its path.
func writeModule(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write module %s: %v", name, err)
	}
}

// T6.1 — a module with a single [return] of a string literal binds that string.
func TestCallSingleReturnValue(t *testing.T) {
	mk, root, _ := newCallScheduler(t)
	writeModule(t, root, "child.ann", "# ann v0.3\n[return] \"ok\"\n")
	s := mk(newStub())
	prog := parseProg(t, "$x = call \"child.ann\"\n[return] --id=out $x\n")
	res := s.Run(prog, "r1", filepath.Join(root, "parent.ann"))
	if res.Esc != nil {
		t.Fatalf("escalated: %s", res.Esc.Format())
	}
	if !strings.Contains(res.Report, "ok") {
		t.Errorf("report missing single-return value:\n%s", res.Report)
	}
}

// T6.2 — a module with two labeled [return]s binds a KMap keyed by --id.
func TestCallMultiReturnKMap(t *testing.T) {
	mk, root, _ := newCallScheduler(t)
	writeModule(t, root, "child.ann",
		"# ann v0.3\n[return] --id=a \"AAA\"\n[return] --id=b \"BBB\"\n")
	s := mk(newStub())
	prog := parseProg(t, "$x = call \"child.ann\"\n[return] --id=out $x\n")
	res := s.Run(prog, "r1", filepath.Join(root, "parent.ann"))
	if res.Esc != nil {
		t.Fatalf("escalated: %s", res.Esc.Format())
	}
	if !strings.Contains(res.Report, "AAA") || !strings.Contains(res.Report, "BBB") {
		t.Errorf("report missing KMap entries:\n%s", res.Report)
	}
	if !strings.Contains(res.Report, "a:") || !strings.Contains(res.Report, "b:") {
		t.Errorf("report missing KMap keys:\n%s", res.Report)
	}
}

// T6.3 — a bare `call` executes the module (its spawns run) but binds nothing.
func TestCallBareExecutesNoBinding(t *testing.T) {
	mk, root, _ := newCallScheduler(t)
	writeModule(t, root, "child.ann",
		"# ann v0.3\n[echo] --id=inner : \"hi\"\n[return] \"done\"\n")
	stub := newStub()
	s := mk(stub)
	prog := parseProg(t, "call \"child.ann\"\n[return] $x\n")
	res := s.Run(prog, "r1", filepath.Join(root, "parent.ann"))
	if res.Esc != nil {
		t.Fatalf("escalated: %s", res.Esc.Format())
	}
	if stub.calls["inner"] != 1 {
		t.Errorf("module dispatch ran %d times, want 1; calls=%v", stub.calls["inner"], stub.calls)
	}
	if !strings.Contains(res.Report, "unbound") {
		t.Errorf("bare call must not bind $x (expected unbound notice):\n%s", res.Report)
	}
}

// T6.4 — RAM isolation: the parent's bindings are invisible to the child and the
// child's bindings never leak back to the parent.
func TestCallRAMIsolation(t *testing.T) {
	mk, root, _ := newCallScheduler(t)
	writeModule(t, root, "child.ann", "# ann v0.3\n$c = \"CVAL\"\n[return] $p\n")
	s := mk(newStub())
	// Parent sets $p; the child returns $p (unbound in the child) and sets $c;
	// the parent then returns $c (unbound in the parent).
	prog := parseProg(t,
		"$p = \"PVAL\"\n$x = call \"child.ann\"\n[return] --id=leak $c\n")
	res := s.Run(prog, "r1", filepath.Join(root, "parent.ann"))
	if res.Esc != nil {
		t.Fatalf("escalated: %s", res.Esc.Format())
	}
	if strings.Contains(res.Report, "PVAL") {
		t.Errorf("parent binding leaked into the child return:\n%s", res.Report)
	}
	if strings.Contains(res.Report, "CVAL") {
		t.Errorf("child binding leaked back to the parent:\n%s", res.Report)
	}
}

// callResumeProgram: a checkpoint-worthy seed, then a call whose child fails in
// run 1 and succeeds on resume. Statement indices:
//
//	0  $a = [echo] --id=a : "x"   (referenced by step 1 -> checkpoint before it)
//	1  $b = [echo] --id=b : "$a"
//	2  $x = call "child.ann"       (the call; fails run 1, succeeds run 2)
//	3  [return] --id=out $x
const callResumeProgram = "$a = [echo] --id=a : \"x\"\n" +
	"$b = [echo] --id=b : \"$a\"\n" +
	"$x = call \"child.ann\"\n" +
	"[return] --id=out $x\n"

// T6.5 — a failing child escalates in the parent, the checkpoint never records
// the call as a completed step, and a resume re-executes the whole call.
func TestCallFailEscalatesResumeReexecutes(t *testing.T) {
	const programPath = "call-resume.ann"
	mk, root, memDir := newCallScheduler(t)
	pp := filepath.Join(root, programPath)

	// Run 1: the child's inner dispatch returns an unhandled error -> Class B.
	writeModule(t, root, "child.ann",
		"# ann v0.3\n[echo] --id=inner : \"go\"\n[return] \"child-ok\"\n")
	failStub := newStub()
	failStub.byID["inner"] = []string{errEnv("inner", "boom")}
	failRes := mk(failStub).Run(parseProg(t, callResumeProgram), "fail", pp)

	failCP, cpOK := checkpoint.Load(memDir, pp)

	// Run 2: resume with a healthy child (stub default success).
	resumeStub := newStub()
	resumeRes := mk(resumeStub).Run(parseProg(t, callResumeProgram), "resume", pp)

	if failRes.Esc == nil || failRes.Esc.Class != 'B' {
		t.Fatalf("failing child must raise Class B in the parent, got %+v", failRes.Esc)
	}
	// No surviving checkpoint may record the call (step 2) as completed.
	if cpOK && failCP.LastCompletedStep >= 2 {
		t.Errorf("checkpoint marked the call completed: last_completed_step=%d", failCP.LastCompletedStep)
	}
	if resumeRes.Esc != nil {
		t.Fatalf("resume escalated: %s", resumeRes.Esc.Format())
	}
	if resumeRes.Status != "success" {
		t.Fatalf("resume status = %q, want success", resumeRes.Status)
	}
	// The call re-executed on resume: the child's inner dispatch ran again.
	if resumeStub.calls["inner"] != 1 {
		t.Errorf("resume did not re-execute the call; inner calls=%v", resumeStub.calls)
	}
}

// T6.6 — a module that itself contains a call exceeds the depth-1 limit: Class B.
func TestCallDepthGuard(t *testing.T) {
	mk, root, _ := newCallScheduler(t)
	writeModule(t, root, "child.ann", "# ann v0.3\ncall \"grand.ann\"\n")
	writeModule(t, root, "grand.ann", "# ann v0.3\n[return] \"deep\"\n")
	s := mk(newStub())
	prog := parseProg(t, "$x = call \"child.ann\"\n")
	res := s.Run(prog, "r1", filepath.Join(root, "parent.ann"))
	if res.Esc == nil || res.Esc.Class != 'B' {
		t.Fatalf("nested call must raise Class B, got %+v", res.Esc)
	}
	if !strings.Contains(strings.ToLower(res.Esc.Detail), "depth") {
		t.Errorf("escalation detail should mention depth: %q", res.Esc.Detail)
	}
}

// T6.7 — a path escaping the program directory is a Class B stop.
func TestCallPathTraversal(t *testing.T) {
	mk, root, _ := newCallScheduler(t)
	s := mk(newStub())
	prog := parseProg(t, "$x = call \"../outside.ann\"\n")
	res := s.Run(prog, "r1", filepath.Join(root, "parent.ann"))
	if res.Esc == nil || res.Esc.Class != 'B' {
		t.Fatalf("path traversal must raise Class B, got %+v", res.Esc)
	}
}

// T6.8 — a missing module and a wrong version header both escalate Class B with
// the call site's line in the detail.
func TestCallLoadErrors(t *testing.T) {
	mk, root, _ := newCallScheduler(t)
	t.Run("missing_file", func(t *testing.T) {
		s := mk(newStub())
		prog := parseProg(t, "$x = call \"nope.ann\"\n")
		res := s.Run(prog, "r1", filepath.Join(root, "parent.ann"))
		if res.Esc == nil || res.Esc.Class != 'B' {
			t.Fatalf("missing module must raise Class B, got %+v", res.Esc)
		}
		if !strings.Contains(res.Esc.Detail, "line 1") {
			t.Errorf("detail should carry the call line: %q", res.Esc.Detail)
		}
	})
	t.Run("wrong_header", func(t *testing.T) {
		writeModule(t, root, "old.ann", "# ann v0.9\n[return] \"x\"\n")
		s := mk(newStub())
		prog := parseProg(t, "$x = call \"old.ann\"\n")
		res := s.Run(prog, "r1", filepath.Join(root, "parent.ann"))
		if res.Esc == nil || res.Esc.Class != 'B' {
			t.Fatalf("wrong header must raise Class B, got %+v", res.Esc)
		}
	})
}

// T6.9 — the child's run directories live under <runID>/call-<n>/.
func TestCallRunDirsNamespaced(t *testing.T) {
	mk, root, memDir := newCallScheduler(t)
	writeModule(t, root, "child.ann",
		"# ann v0.3\n[echo] --id=inner : \"hi\"\n[return] \"ok\"\n")
	s := mk(newStub())
	prog := parseProg(t, "call \"child.ann\"\n")
	res := s.Run(prog, "r1", filepath.Join(root, "parent.ann"))
	if res.Esc != nil {
		t.Fatalf("escalated: %s", res.Esc.Format())
	}
	want := filepath.Join(memDir, "runs", "r1", "call-1", "inner", "prompt.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected child run dir at %s: %v", want, err)
	}
}

// T6.11 — the child's [return]s never appear in the parent report.
func TestCallReturnsDoNotLeakToReport(t *testing.T) {
	mk, root, _ := newCallScheduler(t)
	writeModule(t, root, "child.ann", "# ann v0.3\n[return] \"SECRET-CHILD\"\n")
	s := mk(newStub())
	prog := parseProg(t, "call \"child.ann\"\n[return] \"parent-out\"\n")
	res := s.Run(prog, "r1", filepath.Join(root, "parent.ann"))
	if res.Esc != nil {
		t.Fatalf("escalated: %s", res.Esc.Format())
	}
	if strings.Contains(res.Report, "SECRET-CHILD") {
		t.Errorf("child [return] leaked into the parent report:\n%s", res.Report)
	}
	if !strings.Contains(res.Report, "parent-out") {
		t.Errorf("parent's own [return] missing from report:\n%s", res.Report)
	}
}
