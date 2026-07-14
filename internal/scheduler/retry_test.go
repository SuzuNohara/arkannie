package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"arkannie/internal/config"
	"arkannie/internal/registry"
	"arkannie/internal/spawn"
)

// ---------------------------------------------------------------------------
// Test doubles specific to the declarative retry loop (R12/R13/R14).
// ---------------------------------------------------------------------------

// errEnvHard is a well-formed but NON-recoverable error envelope: it satisfies
// §2 check 5 (reason + recoverable present) yet must never be retried.
func errEnvHard(id, reason string) string {
	return fmt.Sprintf("id: %s\nstatus: error\npayload:\n  reason: %q\n  recoverable: false\n", id, reason)
}

// timeoutSpawner times out for the first failFor attempts, then returns a valid
// success envelope. It exercises the §4.3 timeout synthesis feeding the retry
// loop, without any real time passing.
type timeoutSpawner struct {
	mu      sync.Mutex
	calls   int
	failFor int
}

func (s *timeoutSpawner) Run(_ context.Context, spec spawn.RunSpec) (spawn.Result, error) {
	s.mu.Lock()
	n := s.calls
	s.calls++
	s.mu.Unlock()
	if n < s.failFor {
		return spawn.Result{TimedOut: true}, nil
	}
	prompt, _ := os.ReadFile(spec.PromptFile)
	id := ""
	if m := idRe.FindSubmatch(prompt); m != nil {
		id = string(m[1])
	}
	out, _ := json.Marshal(map[string]string{"result": okEnv(id, "out", "done")})
	return spawn.Result{Stdout: out}, nil
}

// newExecutorScheduler builds a scheduler over a single executor-scope agent,
// mirroring TestExecutorRetryReadOnly's setup, for the executor→Class B guard.
func newExecutorScheduler(t *testing.T, stub spawn.Spawner, name string) *Scheduler {
	t.Helper()
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents")
	writeExecutorAgent(t, agentsDir, name)
	reg, errs := registry.Load(agentsDir)
	if len(errs) > 0 {
		t.Fatalf("registry load: %v", errs)
	}
	cfg := &config.Config{TimeoutDefault: 60, MaxConcurrency: 4, ClaudeBin: "claude", Root: root}
	return New(reg, cfg, stub, filepath.Join(root, ".mem"),
		filepath.Join(agentsDir, ".personalities"), root, spawn.Consent{Workspace: true})
}

// ---------------------------------------------------------------------------
// TestDeclarativeRetry — R12 (--retry), R13 (--backoff), R14 (executor guard).
// ---------------------------------------------------------------------------

func TestDeclarativeRetry(t *testing.T) {
	// T5.1: --retry=2 with two recoverable errors then success → exactly 3
	// spawns and the program continues in success.
	t.Run("three_of_n_exact", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{errEnv("a", "e1"), errEnv("a", "e2"), okEnv("a", "out", "ok")}
		s := newTestScheduler(t, stub, "echo")
		res := s.Run(parseProg(t, "[echo] --id=a --retry=2 : \"x\"\n"), "t1", "")
		if res.Esc != nil {
			t.Fatalf("retry should have recovered, got: %s", res.Esc.Format())
		}
		if stub.calls["a"] != 3 {
			t.Errorf("attempts = %d, want 3 (2 retries then success)", stub.calls["a"])
		}
		if res.Status != "success" {
			t.Errorf("status = %q, want success", res.Status)
		}
	})

	// T5.2: a non-recoverable error is never retried — one spawn, then normal
	// unhandled-error escalation.
	t.Run("non_recoverable_no_retry", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{errEnvHard("a", "fatal")}
		s := newTestScheduler(t, stub, "echo")
		res := s.Run(parseProg(t, "[echo] --id=a --retry=2 : \"x\"\n"), "t2", "")
		if stub.calls["a"] != 1 {
			t.Errorf("attempts = %d, want 1 (non-recoverable: no retry)", stub.calls["a"])
		}
		if res.Esc == nil || res.Esc.Class != 'B' || res.Esc.Title != "unhandled wave error" {
			t.Fatalf("want Class B unhandled error, got %+v", res.Esc)
		}
	})

	// T5.3: a timeout IS retried (timeout synthesizes a recoverable error).
	t.Run("timeout_retries", func(t *testing.T) {
		sp := &timeoutSpawner{failFor: 1}
		s := newTestScheduler(t, sp, "echo")
		res := s.Run(parseProg(t, "[echo] --id=a --retry=1 : \"x\"\n"), "t3", "")
		if res.Esc != nil {
			t.Fatalf("timeout retry should have recovered, got: %s", res.Esc.Format())
		}
		if sp.calls != 2 {
			t.Errorf("attempts = %d, want 2 (timeout then success)", sp.calls)
		}
		if res.Status != "success" {
			t.Errorf("status = %q, want success", res.Status)
		}
	})

	// T5.4: once the N retries are exhausted the last error envelope stays
	// catchable via error -> {} — not a fatal Class B escalation.
	t.Run("exhausted_is_catchable", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{errEnv("a", "e1"), errEnv("a", "e2"), errEnv("a", "e3")}
		s := newTestScheduler(t, stub, "echo")
		src := "[echo] --id=a --retry=2 : \"x\"\n  error -> {\n    [notify] : \"caught\"\n  }\n"
		res := s.Run(parseProg(t, src), "t4", "")
		if res.Esc != nil {
			t.Fatalf("exhausted retry must be catchable, got: %s", res.Esc.Format())
		}
		if stub.calls["a"] != 3 {
			t.Errorf("attempts = %d, want 3 (1 + 2 retries)", stub.calls["a"])
		}
		if countContaining(s.Notices, "caught") != 1 {
			t.Errorf("error handler did not catch exhausted retry; notices=%v", s.Notices)
		}
	})

	// T5.5: --retry=2 --backoff=2 waits 2s then 4s, recorded through the
	// injectable sleep. If the implementation called time.Sleep directly the
	// recorder would stay empty and this test fails.
	t.Run("backoff_schedule", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{errEnv("a", "e1"), errEnv("a", "e2"), okEnv("a", "out", "ok")}
		s := newTestScheduler(t, stub, "echo")
		var sleeps []time.Duration
		s.sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
		res := s.Run(parseProg(t, "[echo] --id=a --retry=2 --backoff=2 : \"x\"\n"), "t5", "")
		if res.Esc != nil {
			t.Fatalf("backoff retry should have recovered, got: %s", res.Esc.Format())
		}
		if len(sleeps) != 2 || sleeps[0] != 2*time.Second || sleeps[1] != 4*time.Second {
			t.Errorf("backoff schedule = %v, want [2s 4s]", sleeps)
		}
		if stub.calls["a"] != 3 {
			t.Errorf("attempts = %d, want 3", stub.calls["a"])
		}
	})

	// T5.6: --retry on an executor is a pre-dispatch Class B stop — nothing is
	// ever spawned (re-executing an executor would double-apply side effects).
	t.Run("executor_retry_class_b", func(t *testing.T) {
		stub := newStub()
		s := newExecutorScheduler(t, stub, "exectool")
		res := s.Run(parseProg(t, "[exectool] --id=x --retry=2 : \"work\"\n"), "t6", "")
		if res.Esc == nil || res.Esc.Class != 'B' {
			t.Fatalf("want Class B pre-dispatch, got %+v", res.Esc)
		}
		if !strings.Contains(res.Esc.Detail, "executor") {
			t.Errorf("detail should explain the executor restriction, got %q", res.Esc.Detail)
		}
		if stub.total() != 0 {
			t.Errorf("executor + --retry must not spawn; calls=%d", stub.total())
		}
	})

	// T5.7: an attempt whose envelope violates the protocol still gets its
	// internal corrective retry, and that does NOT consume a declarative retry.
	t.Run("corrective_not_counted", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{"garbage not an envelope", okEnv("a", "out", "recovered")}
		s := newTestScheduler(t, stub, "echo")
		res := s.Run(parseProg(t, "[echo] --id=a --retry=2 : \"x\"\n"), "t7", "")
		if res.Esc != nil {
			t.Fatalf("corrective retry should have recovered, got: %s", res.Esc.Format())
		}
		if stub.calls["a"] != 2 {
			t.Errorf("attempts = %d, want 2 (corrective retry within ONE declarative attempt)", stub.calls["a"])
		}
	})

	// T5.8: regression — a recoverable error without --retry is a single
	// attempt with identical semantics to today (default retries = 0).
	t.Run("regression_no_retry", func(t *testing.T) {
		stub := newStub()
		stub.byID["a"] = []string{errEnv("a", "boom")}
		s := newTestScheduler(t, stub, "echo")
		src := "[echo] --id=a : \"x\"\n  error -> {\n    [notify] : \"handled\"\n  }\n"
		res := s.Run(parseProg(t, src), "t8", "")
		if res.Esc != nil {
			t.Fatalf("unexpected escalation: %s", res.Esc.Format())
		}
		if stub.calls["a"] != 1 {
			t.Errorf("attempts = %d, want 1 (no --retry)", stub.calls["a"])
		}
		if countContaining(s.Notices, "handled") != 1 {
			t.Errorf("error handler did not run; notices=%v", s.Notices)
		}
	})
}
