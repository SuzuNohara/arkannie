package scheduler

import (
	"path/filepath"
	"testing"

	"arkannie/internal/config"
	"arkannie/internal/ram"
	"arkannie/internal/registry"
	"arkannie/internal/spawn"

	"arkannie/internal/checkpoint"
)

// ifResumeProgram is the §8/§10 fixture for R13: a top-level `if` sits between
// two dispatch bindings. The guard reads $a (a completed top-level binding), the
// then-branch performs a side-effect dispatch (id `t`), and a later dispatch
// (id `c`) is the failure point. The final [return] renders $a — a value the
// checkpoint must carry across the fail/resume boundary.
//
// Statement indices (top level):
//
//	0  $a = [echo] --id=a       (Assign, referenced by the guard -> checkpointed)
//	1  if $a.out == "AA" { [echo] --id=t }   (If, one completed top-level step)
//	2  $b = [echo] --id=b       (Assign, referenced by step 3 -> checkpointed)
//	3  [echo] --id=c : "$b"     (Dispatch, the mid-program failure point)
//	4  [return] --id=out $a     (final output, driven by the restored $a)
const ifResumeProgram = "$a = [echo] --id=a : \"x\"\n" +
	"if $a.out == \"AA\" {\n" +
	"  [echo] --id=t : \"then\"\n" +
	"}\n" +
	"$b = [echo] --id=b : \"y\"\n" +
	"[echo] --id=c : \"$b\"\n" +
	"[return] --id=out $a\n"

// newSharedScheduler builds Scheduler instances that share one memDir (and thus
// one checkpoint store) and registry, so a failing run and a later resume run —
// each with its own stub Spawner — see the same on-disk checkpoint. This mirrors
// newTestSchedulerSuccess but returns a factory instead of a single scheduler.
func newSharedScheduler(t *testing.T) (mk func(spawn.Spawner) *Scheduler, memDir string) {
	t.Helper()
	root := t.TempDir()
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
	return mk, memDir
}

// TestIfTopLevelCheckpointResume pins R13: a program mode run that contains a
// top-level `if` and fails partway persists a checkpoint that resumes to
// completion with the CURRENT checkpoint schema — no schema or productive-code
// change. The `if` counts as one completed top-level step, so a resume that
// starts past it must NOT re-evaluate the guard or re-fire the branch's
// side-effect dispatch, yet must reproduce the same final result as a clean run.
func TestIfTopLevelCheckpointResume(t *testing.T) {
	const programPath = "if-resume.ann"

	// --- Reference run: no failure, single pass (non-program mode). ---------
	refStub := newStub()
	refStub.byID["a"] = []string{okEnv("a", "out", "AA")}
	refS := newTestScheduler(t, refStub, "echo")
	refRes := refS.Run(parseProg(t, ifResumeProgram), "ref", "")
	if refRes.Esc != nil {
		t.Fatalf("reference run escalated: %s", refRes.Esc.Format())
	}
	if refRes.Status != "success" {
		t.Fatalf("reference status = %q, want success", refRes.Status)
	}
	if refStub.calls["t"] != 1 {
		t.Fatalf("reference: then-branch dispatch should run once; calls=%v", refStub.calls)
	}

	// --- Fail + resume across a shared checkpoint store. --------------------
	mk, memDir := newSharedScheduler(t)

	// Phase 1: fail mid-program. `c` returns a valid error envelope with no
	// handler -> Class B escalation after `$a`, the `if`, and `$b` completed.
	failStub := newStub()
	failStub.byID["a"] = []string{okEnv("a", "out", "AA")}
	failStub.byID["c"] = []string{errEnv("c", "mid-program boom")}
	failS := mk(failStub)
	failRes := failS.Run(parseProg(t, ifResumeProgram), "fail", programPath)

	// Inspect the surviving checkpoint NOW, before the resume run cleans it.
	failCP, failCPok := checkpoint.Load(memDir, programPath)

	// Phase 2: resume. `c` now succeeds (stub default). Same memDir/checkpoint.
	resumeStub := newStub()
	resumeStub.byID["a"] = []string{okEnv("a", "out", "AA")} // must NOT be reached
	resumeS := mk(resumeStub)
	resumeRes := resumeS.Run(parseProg(t, ifResumeProgram), "resume", programPath)

	t.Run("failing_run_escalates_and_persists_current_schema_checkpoint", func(t *testing.T) {
		if failRes.Esc == nil || failRes.Esc.Class != 'B' {
			t.Fatalf("failing run must raise a Class B escalation, got %+v", failRes.Esc)
		}
		// The failed run wrote a checkpoint that the CURRENT deserializer reads
		// with no format change (§10.3) — this is the crux of R13.
		if !failCPok {
			t.Fatal("checkpoint not persisted after mid-program failure")
		}
		// The surviving checkpoint is the one written before `$b` (step index 2),
		// recording steps 0 and 1 (the assign and the whole `if`) as completed.
		if failCP.LastCompletedStep != 1 {
			t.Fatalf("last_completed_step = %d, want 1 (assign + if completed)", failCP.LastCompletedStep)
		}
		if failCP.Program != programPath {
			t.Errorf("checkpoint program = %q, want %q", failCP.Program, programPath)
		}
		// $a — the pre-`if` binding the guard read — is carried in the snapshot,
		// deserialized as the same map value the live run produced.
		av, has := failCP.Bindings["a"]
		if !has {
			t.Fatalf("checkpoint bindings missing $a: %v", failCP.Bindings)
		}
		if av.Kind != ram.KMap || av.Map["out"].Kind != ram.KString || av.Map["out"].Str != "AA" {
			t.Errorf("restored $a = %+v, want map{out: \"AA\"}", av)
		}
		// Branch-local bindings never escape the `if`, so nothing spurious leaks
		// into the snapshot; $b was assigned AFTER this checkpoint was written.
		if _, leaked := failCP.Bindings["b"]; leaked {
			t.Errorf("checkpoint must not carry post-checkpoint $b: %v", failCP.Bindings)
		}
	})

	t.Run("resume_completes_without_replaying_the_if", func(t *testing.T) {
		if resumeRes.Esc != nil {
			t.Fatalf("resume escalated: %s", resumeRes.Esc.Format())
		}
		if resumeRes.Status != "success" {
			t.Fatalf("resume status = %q, want success", resumeRes.Status)
		}
		// The `if` (step 1) and the seed assign (step 0) are already complete:
		// resume starts at step 2, so neither the guard's binding nor the
		// then-branch side-effect dispatch may re-fire — no double execution.
		if _, ran := resumeStub.calls["a"]; ran {
			t.Errorf("step 0 (id a) re-dispatched on resume; calls=%v", resumeStub.calls)
		}
		if _, ran := resumeStub.calls["t"]; ran {
			t.Errorf("completed if branch (id t) re-fired on resume; calls=%v", resumeStub.calls)
		}
		// The remaining steps DO run exactly once.
		if resumeStub.calls["b"] != 1 || resumeStub.calls["c"] != 1 {
			t.Errorf("resume did not run remaining steps once; calls=%v", resumeStub.calls)
		}
		// Success clears the checkpoint (§10.5).
		if _, ok := checkpoint.Load(memDir, programPath); ok {
			t.Error("checkpoint not cleaned after successful resume")
		}
	})

	t.Run("resume_result_matches_clean_run", func(t *testing.T) {
		if resumeRes.Report != refRes.Report {
			t.Errorf("resume report differs from clean run:\n--- resume ---\n%q\n--- clean ---\n%q",
				resumeRes.Report, refRes.Report)
		}
	})
}
