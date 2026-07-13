package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCheckProg writes a .ann fixture under dir and returns its path. Fixtures
// are generated in the test's temp root (never in testdata/), each carrying the
// v0.2 header on line 1.
func writeCheckProg(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	mustWrite(t, p, body)
	return p
}

// assertNoSideEffects fails if --check touched the execution surfaces: it must
// never create .output/ (no run report) nor .mem/ (no scheduler/healthcheck).
func assertNoSideEffects(t *testing.T, root string) {
	t.Helper()
	for _, dir := range []string{".output", ".mem"} {
		if _, err := os.Stat(filepath.Join(root, dir)); !os.IsNotExist(err) {
			t.Fatalf("--check must not create %s/ (err=%v)", dir, err)
		}
	}
}

// T4.1 — a valid program parses: exit 0, stdout carries OK + the "syntax only"
// disclaimer, no agent is dispatched, and no side-effect directory is created.
func TestCheckValidProgram(t *testing.T) {
	sp := &stubSpawner{}
	app, out, errb := newTestApp(t, sp)
	prog := writeCheckProg(t, app.Root, "ok.ann", "# ann v0.2\n[echo]: hi from program\n")

	code := app.Run([]string{"--check", prog})

	if code != 0 {
		t.Fatalf("valid program --check should exit 0, got %d (stderr: %q)", code, errb.String())
	}
	s := out.String()
	if !strings.Contains(s, "OK") {
		t.Fatalf("stdout should report OK:\n%s", s)
	}
	if !strings.Contains(s, "syntax only") {
		t.Fatalf("stdout should carry the 'syntax only' disclaimer:\n%s", s)
	}
	if sp.calls != 0 {
		t.Fatalf("--check must not dispatch any agent, calls = %d", sp.calls)
	}
	assertNoSideEffects(t, app.Root)
}

// T4.2 — a syntax error is reported to stderr in the canonical
// `parse error at L:C [category]: msg` form with exit 1 and zero side-effects.
func TestCheckParseError(t *testing.T) {
	sp := &stubSpawner{}
	app, out, errb := newTestApp(t, sp)
	prog := writeCheckProg(t, app.Root, "bad.ann", "# ann v0.2\nif something {\n")

	code := app.Run([]string{"--check", prog})

	if code != 1 {
		t.Fatalf("parse error --check should exit 1, got %d", code)
	}
	e := errb.String()
	if !strings.Contains(e, "parse error at") {
		t.Fatalf("stderr should carry 'parse error at':\n%s", e)
	}
	if !strings.Contains(e, ":") || !strings.Contains(e, "[") {
		t.Fatalf("stderr should carry the L:C [category] form:\n%s", e)
	}
	if out.Len() != 0 {
		t.Fatalf("a failing --check must not print to stdout: %q", out.String())
	}
	if sp.calls != 0 {
		t.Fatalf("--check must not dispatch any agent, calls = %d", sp.calls)
	}
	assertNoSideEffects(t, app.Root)
}

// T4.3 — --check is incompatible with the execution flags and requires a .ann
// input; every invalid composition is a usage error (exit 64) that never runs.
func TestCheckInvalidCompositions(t *testing.T) {
	tests := []struct {
		name string
		argv []string
	}{
		{"check_plus_agent", []string{"--check", "--agent=echo", "p.ann"}},
		{"check_plus_forge", []string{"--check", "--forge", "p.ann"}},
		{"check_plus_detach", []string{"--check", "--detach", "p.ann"}},
		{"check_plus_interpret", []string{"--check", "--interpret", "p.ann"}},
		{"check_without_input", []string{"--check"}},
		{"check_with_non_ann_input", []string{"--check", "notes.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sp := &stubSpawner{}
			app, _, errb := newTestApp(t, sp)
			code := app.Run(tc.argv)
			if code != 64 {
				t.Fatalf("%v should be a usage error 64, got %d", tc.argv, code)
			}
			if errb.Len() == 0 {
				t.Fatalf("%v should explain the usage error on stderr", tc.argv)
			}
			if sp.calls != 0 {
				t.Fatalf("invalid --check composition must not dispatch, calls = %d", sp.calls)
			}
			assertNoSideEffects(t, app.Root)
		})
	}
}

// parseArgs decodes --check into the check flag and enforces its constraints.
func TestParseArgsCheck(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		wantCheck bool
		wantInput string
		wantErr   bool
	}{
		{name: "bare_with_ann", argv: []string{"--check", "p.ann"}, wantCheck: true, wantInput: "p.ann"},
		{name: "missing_ann_input", argv: []string{"--check"}, wantErr: true},
		{name: "non_ann_input", argv: []string{"--check", "notes.txt"}, wantErr: true},
		{name: "with_agent_is_error", argv: []string{"--check", "--agent=echo", "p.ann"}, wantErr: true},
		{name: "with_interpret_is_error", argv: []string{"--check", "--interpret", "p.ann"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := parseArgs(tc.argv)
			if tc.wantErr {
				if p.usageErr == "" {
					t.Fatalf("parseArgs(%v) usageErr empty, want error", tc.argv)
				}
				return
			}
			if p.usageErr != "" {
				t.Fatalf("parseArgs(%v) unexpected usageErr %q", tc.argv, p.usageErr)
			}
			if p.check != tc.wantCheck {
				t.Errorf("check = %v, want %v", p.check, tc.wantCheck)
			}
			if p.input != tc.wantInput {
				t.Errorf("input = %q, want %q", p.input, tc.wantInput)
			}
		})
	}
}

// help documents the --check flag in its flag table.
func TestHelpDocumentsCheck(t *testing.T) {
	var b strings.Builder
	printHelp(&b)
	if !strings.Contains(b.String(), "--check") {
		t.Error("help does not document --check")
	}
}
