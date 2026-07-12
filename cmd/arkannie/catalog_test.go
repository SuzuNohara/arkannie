package main

import (
	"strings"
	"testing"
)

func TestParseArgsCatalog(t *testing.T) {
	tests := []struct {
		name         string
		argv         []string
		wantCatalog  bool
		wantAgent    string
		wantInput    string
		wantErr      bool
		errSubstring string
	}{
		{name: "bare", argv: []string{"--catalog"}, wantCatalog: true},
		{name: "with_agent", argv: []string{"--catalog=echo"}, wantCatalog: true, wantAgent: "echo"},
		{name: "empty_value", argv: []string{"--catalog="}, wantErr: true, errSubstring: "missing value"},
		{name: "never_consumes_next", argv: []string{"--catalog", "prog.ann"}, wantCatalog: true, wantInput: "prog.ann"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := parseArgs(tc.argv)
			if tc.wantErr {
				if p.usageErr == "" {
					t.Fatalf("parseArgs(%v) usageErr empty, want error", tc.argv)
				}
				if tc.errSubstring != "" && !strings.Contains(p.usageErr, tc.errSubstring) {
					t.Errorf("usageErr = %q, want substring %q", p.usageErr, tc.errSubstring)
				}
				return
			}
			if p.usageErr != "" {
				t.Fatalf("parseArgs(%v) unexpected usageErr %q", tc.argv, p.usageErr)
			}
			if p.catalog != tc.wantCatalog {
				t.Errorf("catalog = %v, want %v", p.catalog, tc.wantCatalog)
			}
			if p.catalogAgent != tc.wantAgent {
				t.Errorf("catalogAgent = %q, want %q", p.catalogAgent, tc.wantAgent)
			}
			if p.input != tc.wantInput {
				t.Errorf("input = %q, want %q", p.input, tc.wantInput)
			}
		})
	}
}

func TestRunCatalog(t *testing.T) {
	t.Run("pool_exit_0", func(t *testing.T) {
		app, out, _ := newTestApp(t, &e2eSpawner{})
		code := app.Run([]string{"--catalog"})
		if code != 0 {
			t.Fatalf("--catalog exit = %d, want 0", code)
		}
		s := out.String()
		if !strings.Contains(s, "AGENT CATALOG") || !strings.Contains(s, "[echo]") {
			t.Errorf("catalog output missing header/agent:\n%s", s)
		}
		if !strings.Contains(s, "Echo the dispatched text.") {
			t.Errorf("catalog output missing echo purpose:\n%s", s)
		}
	})

	t.Run("single_agent_exit_0", func(t *testing.T) {
		app, out, _ := newTestApp(t, &e2eSpawner{})
		code := app.Run([]string{"--catalog=echo"})
		if code != 0 {
			t.Fatalf("--catalog=echo exit = %d, want 0", code)
		}
		if !strings.Contains(out.String(), "[echo]") {
			t.Errorf("single-agent catalog missing [echo]:\n%s", out.String())
		}
	})

	t.Run("unknown_agent_exit_64", func(t *testing.T) {
		app, _, errb := newTestApp(t, &e2eSpawner{})
		code := app.Run([]string{"--catalog=nope"})
		if code != 64 {
			t.Fatalf("--catalog=nope exit = %d, want 64", code)
		}
		if !strings.Contains(errb.String(), "unknown agent") {
			t.Errorf("stderr missing 'unknown agent':\n%s", errb.String())
		}
	})

	t.Run("does_not_execute_program", func(t *testing.T) {
		sp := &e2eSpawner{}
		app, _, _ := newTestApp(t, sp)
		app.Run([]string{"--catalog", "prog.ann"})
		if sp.calls != 0 {
			t.Errorf("catalog should not dispatch any agent, calls = %d", sp.calls)
		}
	})
}

func TestHelpDocumentsCatalog(t *testing.T) {
	var b strings.Builder
	printHelp(&b)
	if !strings.Contains(b.String(), "--catalog") {
		t.Error("help does not document --catalog")
	}
}

func TestVersionFlag(t *testing.T) {
	app, out, _ := newTestApp(t, &e2eSpawner{})
	code := app.Run([]string{"--version"})
	if code != 0 {
		t.Fatalf("--version exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "arkannie") {
		t.Errorf("--version output = %q, want the version banner", out.String())
	}
}
