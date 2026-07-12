// Package main is the arkannie CLI entry point. It wires config, registry, the
// Ann parser, the interpreter fallback, the scheduler and the output writer
// into one blocking (or detached) run. The App struct isolates all I/O behind
// injectable collaborators so the whole flow is exercised in-process by tests.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"arkannie/internal/config"
	"arkannie/internal/interpreter"
	"arkannie/internal/output"
	"arkannie/internal/spawn"
)

// version is the arkannie release, stamped at build time via
//
//	-ldflags "-X main.version=$(cat VERSION)"
//
// (see the Makefile). It falls back to "dev" for un-stamped builds.
var version = "dev"

// App is the injectable CLI application. Production wiring lives in
// newRealApp; tests construct an App with stub collaborators.
type App struct {
	Root       string                                     // ARKANNIE_HOME
	Cfg        *config.Config                             // loaded config
	Spawner    spawn.Spawner                              // ClaudeSpawner in prod, stub in tests
	Exec       interpreter.ExecFunc                       // interpreter exec, os/exec in prod
	ForkExec   func(args []string) error                  // re-exec for --detach
	RunForge   func(cwd, bin string, argv []string) error // interactive claude for --forge
	InvokerCwd string                                     // cwd where arkannie was invoked (executor scope)
	Stdout     io.Writer
	Stderr     io.Writer
	Now        func() time.Time
}

// Run parses argv and dispatches to the matching mode, returning the process
// exit code. It never calls os.Exit. Exit codes: 0 success, 1 error, 2 info,
// 64 usage error.
func (a *App) Run(argv []string) int {
	args := parseArgs(argv)
	if args.usageErr != "" {
		fmt.Fprintln(a.Stderr, "usage error: "+args.usageErr)
		return 64
	}
	if args.version {
		fmt.Fprintf(a.Stdout, "arkannie %s (Ann v0.1)\n", version)
		return 0
	}
	if args.help {
		printHelp(a.Stdout)
		return 0
	}
	if args.subcommand == "validate" {
		return a.runValidate(args)
	}
	if args.catalog {
		return a.runCatalog(args)
	}
	if args.man {
		return a.runMan(args)
	}
	if args.forge {
		return a.runForge(args)
	}
	if args.input == "" {
		fmt.Fprintln(a.Stderr, "usage error: an input (prompt text or .ann path) is required")
		return 64
	}
	// Program mode (a .ann file) resolves each dispatch's agent from the
	// registry, so --agent is required only for prompt (free-text) mode.
	if !strings.HasSuffix(args.input, ".ann") && args.agent == "" {
		fmt.Fprintln(a.Stderr, "usage error: --agent is required for prompt execution")
		return 64
	}
	if output.SanitizeLabel(args.id) == "" {
		fmt.Fprintln(a.Stderr, "usage error: --id is required for execution")
		return 64
	}
	if args.detach {
		return a.runDetach(argv, args)
	}
	return a.runExecute(args)
}

// now returns the injected clock, defaulting to time.Now.
func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// BuildForgeArgv returns the argv (without the binary) for an interactive
// Agent Forge session: no -p, so claude runs interactively and loads
// CLAUDE.md → arkannie.md from the working directory. When a forge name or an
// absorb path is present, a seed prompt is passed as ONE positional element.
func BuildForgeArgv(args parsedArgs) []string {
	if args.absorb != "" {
		seed := "<absorb> " + args.absorb
		if args.mode != "" {
			seed += " --mode=" + args.mode
		}
		if args.forgeName != "" {
			seed += " --name=" + args.forgeName
		}
		return []string{seed}
	}
	if args.forgeName != "" {
		return []string{"<forge> " + args.forgeName}
	}
	return []string{}
}

// validateAbsorbPath checks that path exists, is a directory and is readable.
func validateAbsorbPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("absorb path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("absorb path %s is not a directory", path)
	}
	if _, err := os.ReadDir(path); err != nil {
		return fmt.Errorf("absorb path not readable: %w", err)
	}
	return nil
}

// runForge launches the interactive Agent Forge session with cwd=Root. A
// relative --absorb path is resolved against InvokerCwd and validated before
// anything is spawned; an invalid path is a usage error (64).
func (a *App) runForge(args parsedArgs) int {
	if args.absorb != "" {
		if !filepath.IsAbs(args.absorb) {
			args.absorb = filepath.Join(a.InvokerCwd, args.absorb)
		}
		if err := validateAbsorbPath(args.absorb); err != nil {
			fmt.Fprintln(a.Stderr, "usage error: "+err.Error())
			return 64
		}
	}
	bin := "claude"
	if a.Cfg != nil && a.Cfg.ClaudeBin != "" {
		bin = a.Cfg.ClaudeBin
	}
	if a.RunForge == nil {
		fmt.Fprintln(a.Stderr, "forge: no interactive runner configured")
		return 1
	}
	if err := a.RunForge(a.Root, bin, BuildForgeArgv(args)); err != nil {
		fmt.Fprintln(a.Stderr, "forge failed: "+err.Error())
		return 1
	}
	return 0
}

// main wires the real App and exits with its status.
func main() {
	os.Exit(newRealApp().Run(os.Args[1:]))
}

// newRealApp assembles the production App: Root from ARKANNIE_HOME (or cwd),
// config loaded from Root, and os/exec-backed collaborators.
func newRealApp() *App {
	root := os.Getenv("ARKANNIE_HOME")
	cwd, _ := os.Getwd()
	if root == "" {
		root = cwd
	}
	cfg, err := config.Load(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config: "+err.Error())
		cfg = &config.Config{TimeoutDefault: 120, MaxConcurrency: 4, ClaudeBin: "claude", Root: root}
	}
	return &App{
		Root:       root,
		Cfg:        cfg,
		Spawner:    &spawn.ClaudeSpawner{Bin: cfg.ClaudeBin},
		Exec:       realExec,
		ForkExec:   realForkExec,
		RunForge:   realRunForge,
		InvokerCwd: cwd,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Now:        time.Now,
	}
}

// realExec runs claude for the interpreter fallback, capturing stdout. A
// non-zero exit is reported through exitCode, not a Go error.
func realExec(ctx context.Context, bin string, args []string, cwd string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out, ee.ExitCode(), nil
		}
		return nil, 0, err
	}
	return out, 0, nil
}

// realForkExec re-execs arkannie itself detached in its own process group so the
// parent can return immediately for --detach.
func realForkExec(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable for detach: %w", err)
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Start()
}

// realRunForge execs claude interactively, inheriting the terminal stdio.
func realRunForge(cwd, bin string, argv []string) error {
	cmd := exec.Command(bin, argv...)
	cmd.Dir = cwd
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runDetach prints the future output path, re-execs arkannie without --detach and
// with the internal --_runid flag, then returns 0 immediately.
func (a *App) runDetach(argv []string, args parsedArgs) int {
	runID := output.NewRunID(args.id)
	// The output filename is the raw (sanitized) --id; the newest run always
	// keeps this clean name, so the path is deterministic here.
	path := filepath.Join(a.Root, ".output", output.SanitizeLabel(args.id)+".md")
	fmt.Fprintln(a.Stdout, path)
	child := append(stripDetach(argv), "--_runid="+runID)
	if a.ForkExec == nil {
		fmt.Fprintln(a.Stderr, "detach: no fork runner configured")
		return 1
	}
	if err := a.ForkExec(child); err != nil {
		fmt.Fprintln(a.Stderr, "detach failed: "+err.Error())
		return 1
	}
	return 0
}

// stripDetach removes the --detach flag (in either form) from an argv copy.
func stripDetach(argv []string) []string {
	out := make([]string, 0, len(argv))
	for _, a := range argv {
		if a == "--detach" || a == "--detach=true" {
			continue
		}
		out = append(out, a)
	}
	return out
}
