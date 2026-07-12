package spawn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func stubPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "claude-stub.sh"))
	if err != nil {
		t.Fatalf("resolving stub path: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("claude-stub.sh not found: %v", err)
	}
	return p
}

func TestArgv(t *testing.T) {
	t.Run("U9-T4_argv_golden", func(t *testing.T) {
		c := &ClaudeSpawner{Bin: "claude"}
		spec := RunSpec{
			PromptFile:      "/run/prompt.md",
			Model:           "claude-haiku-4-5",
			Cwd:             "/run",
			AllowedTools:    []string{"Read", "Grep", "Glob", "WebFetch", "WebSearch"},
			DisallowedTools: []string{"Write", "Edit", "Bash", "NotebookEdit"},
			AddDirs:         []string{"/extra/docs"},
			Timeout:         30 * time.Second,
		}
		want := []string{
			"-p", "Ejecuta la operación descrita en tu system prompt. Devuelve únicamente el envelope YAML.",
			"--model", "claude-haiku-4-5",
			"--append-system-prompt-file", "/run/prompt.md",
			"--output-format", "json",
			"--allowedTools", "Read", "Grep", "Glob", "WebFetch", "WebSearch",
			"--disallowedTools", "Write", "Edit", "Bash", "NotebookEdit",
			"--add-dir", "/extra/docs",
		}
		if got := c.Argv(spec); !reflect.DeepEqual(got, want) {
			t.Errorf("Argv() =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("U9-T4_argv_omits_empty_sections", func(t *testing.T) {
		c := &ClaudeSpawner{Bin: "claude"}
		spec := RunSpec{PromptFile: "/p.md", Model: "m"}
		got := c.Argv(spec)
		for _, flag := range []string{"--allowedTools", "--disallowedTools", "--add-dir"} {
			for _, arg := range got {
				if arg == flag {
					t.Errorf("Argv() contains %s despite empty list: %q", flag, got)
				}
			}
		}
	})
}

func TestRun(t *testing.T) {
	t.Run("U9-T5_hang_timeout_kills_group", func(t *testing.T) {
		dir := t.TempDir()
		pidFile := filepath.Join(dir, "stub.pid")
		t.Setenv("STUB_MODE", "hang")
		t.Setenv("STUB_PIDFILE", pidFile)
		c := &ClaudeSpawner{Bin: stubPath(t)}
		spec := RunSpec{Cwd: dir, Model: "m", PromptFile: "/dev/null", Timeout: time.Second}
		start := time.Now()
		res, err := c.Run(context.Background(), spec)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("Run: unexpected error %v", err)
		}
		if !res.TimedOut {
			t.Error("TimedOut = false, want true")
		}
		if elapsed >= 8*time.Second {
			t.Errorf("Run took %v, want < 8s", elapsed)
		}
		raw, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatalf("reading stub pidfile: %v", err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			t.Fatalf("parsing stub pid: %v", err)
		}
		if kerr := syscall.Kill(pid, 0); !errors.Is(kerr, syscall.ESRCH) {
			t.Errorf("stub pid %d still alive: kill(pid,0) = %v, want ESRCH", pid, kerr)
		}
		waitGroupGone(t, pid)
	})

	t.Run("U9-T6_fail_exit3_stderr_captured", func(t *testing.T) {
		t.Setenv("STUB_MODE", "fail")
		c := &ClaudeSpawner{Bin: stubPath(t)}
		spec := RunSpec{Cwd: t.TempDir(), Model: "m", PromptFile: "/dev/null", Timeout: 30 * time.Second}
		res, err := c.Run(context.Background(), spec)
		if err != nil {
			t.Fatalf("Run: unexpected error %v", err)
		}
		if res.ExitCode != 3 {
			t.Errorf("ExitCode = %d, want 3", res.ExitCode)
		}
		if !strings.Contains(string(res.Stderr), "stub: simulated failure") {
			t.Errorf("Stderr = %q, want stub failure message", res.Stderr)
		}
		if res.TimedOut {
			t.Error("TimedOut = true, want false")
		}
	})

	t.Run("echo_roundtrip_argv_via_stub", func(t *testing.T) {
		t.Setenv("STUB_MODE", "echo")
		c := &ClaudeSpawner{Bin: stubPath(t)}
		spec := RunSpec{
			Cwd:             t.TempDir(),
			Model:           "claude-sonnet-4-5",
			PromptFile:      "/run/prompt.md",
			AllowedTools:    []string{"Read", "Grep", "Glob"},
			DisallowedTools: []string{"Write", "Edit", "Bash", "NotebookEdit"},
			Timeout:         30 * time.Second,
		}
		res, err := c.Run(context.Background(), spec)
		if err != nil {
			t.Fatalf("Run: unexpected error %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("ExitCode = %d, stderr = %q", res.ExitCode, res.Stderr)
		}
		var got []string
		if err := json.Unmarshal(res.Stdout, &got); err != nil {
			t.Fatalf("stub stdout is not JSON argv: %v (%q)", err, res.Stdout)
		}
		if want := c.Argv(spec); !reflect.DeepEqual(got, want) {
			t.Errorf("stub saw argv %q, want %q", got, want)
		}
	})

	t.Run("ctx_cancel_kills_group", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("STUB_MODE", "hang")
		t.Setenv("STUB_PIDFILE", filepath.Join(dir, "stub.pid"))
		c := &ClaudeSpawner{Bin: stubPath(t)}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		res, err := c.Run(ctx, RunSpec{Cwd: dir, Model: "m", PromptFile: "/dev/null"})
		if time.Since(start) >= 8*time.Second {
			t.Errorf("Run took %v after ctx cancel, want < 8s", time.Since(start))
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run error = %v, want context.Canceled", err)
		}
		if res.TimedOut {
			t.Error("TimedOut = true on ctx cancel, want false")
		}
	})

	t.Run("start_failure_returns_error", func(t *testing.T) {
		c := &ClaudeSpawner{Bin: filepath.Join(t.TempDir(), "no-such-bin")}
		_, err := c.Run(context.Background(), RunSpec{Cwd: t.TempDir(), Model: "m"})
		if err == nil {
			t.Fatal("Run: want start error, got nil")
		}
	})
}

// waitGroupGone polls kill(-pgid, 0) until the whole process group is gone.
// A killed grandchild may linger as a zombie (still a group member) until
// init reaps it, so a bounded poll — not a fixed sleep — observes the final
// kernel state deterministically.
func waitGroupGone(t *testing.T, pgid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := syscall.Kill(-pgid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("process group %d still alive after kill", pgid)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
