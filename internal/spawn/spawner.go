package spawn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// dispatchPrompt is the fixed -p prompt for every agent spawn; the real
// instructions live in the appended system prompt file.
const dispatchPrompt = "Ejecuta la operación descrita en tu system prompt. Devuelve únicamente el envelope YAML."

// killGrace is how long a timed-out process group gets to honor SIGTERM
// before it is SIGKILLed.
const killGrace = 5 * time.Second

// Result carries everything the scheduler needs from one finished spawn.
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int
	TimedOut       bool
}

// Spawner runs one RunSpec to completion.
type Spawner interface {
	Run(ctx context.Context, s RunSpec) (Result, error)
}

// ClaudeSpawner spawns the claude CLI (or a test stub) as an isolated
// process group so that timeout kills reach every descendant.
type ClaudeSpawner struct{ Bin string }

// Argv returns the full argument list for s, without the binary itself.
// Exported so tests and the scheduler can inspect the exact invocation.
func (c *ClaudeSpawner) Argv(s RunSpec) []string {
	argv := []string{
		"-p", dispatchPrompt,
		"--model", s.Model,
		"--append-system-prompt-file", s.PromptFile,
		"--output-format", "json",
	}
	if len(s.AllowedTools) > 0 {
		argv = append(argv, "--allowedTools")
		argv = append(argv, s.AllowedTools...)
	}
	if len(s.DisallowedTools) > 0 {
		argv = append(argv, "--disallowedTools")
		argv = append(argv, s.DisallowedTools...)
	}
	for _, dir := range s.AddDirs {
		argv = append(argv, "--add-dir", dir)
	}
	return argv
}

// Run executes s to completion. A non-zero exit without timeout is not a Go
// error: the caller reads Result.ExitCode and decides. Timeout and context
// cancellation both kill the whole process group (SIGTERM, grace, SIGKILL);
// only the timeout path sets Result.TimedOut.
func (c *ClaudeSpawner) Run(ctx context.Context, s RunSpec) (Result, error) {
	cmd := exec.Command(c.Bin, c.Argv(s)...)
	cmd.Dir = s.Cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("starting %s: %w", c.Bin, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	waitErr, timedOut, ctxErr := awaitOrKill(ctx, cmd.Process.Pid, s.Timeout, done)
	res, err := buildResult(&stdout, &stderr, waitErr, timedOut)
	if err != nil {
		return res, err
	}
	return res, ctxErr
}

// awaitOrKill waits for the child to exit, the timeout to fire, or the
// context to be cancelled — killing the process group in the latter two.
func awaitOrKill(ctx context.Context, pgid int, timeout time.Duration, done <-chan error) (waitErr error, timedOut bool, ctxErr error) {
	var timeoutC <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timeoutC = t.C
	}
	select {
	case err := <-done:
		return err, false, nil
	case <-timeoutC:
		return killGroup(pgid, done), true, nil
	case <-ctx.Done():
		return killGroup(pgid, done), false, ctx.Err()
	}
}

// killGroup sends SIGTERM to the process group, waits killGrace for a clean
// exit, then SIGKILLs the group. It returns once cmd.Wait has reaped the
// child, so captured output buffers are safe to read afterwards.
func killGroup(pgid int, done <-chan error) error {
	_ = syscall.Kill(-pgid, syscall.SIGTERM) // best effort; group may be gone
	grace := time.NewTimer(killGrace)
	defer grace.Stop()
	select {
	case err := <-done:
		return err
	case <-grace.C:
		_ = syscall.Kill(-pgid, syscall.SIGKILL) // best effort; group may be gone
		return <-done
	}
}

// buildResult folds cmd.Wait's outcome into a Result. Non-zero exits become
// Result.ExitCode without a Go error; anything else (I/O failure) is an error.
func buildResult(stdout, stderr *bytes.Buffer, waitErr error, timedOut bool) (Result, error) {
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), TimedOut: timedOut}
	if waitErr == nil {
		return res, nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	return res, fmt.Errorf("waiting for spawn: %w", waitErr)
}
