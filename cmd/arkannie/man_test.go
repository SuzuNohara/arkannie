package main

import (
	"strings"
	"testing"
)

func TestParseArgsMan(t *testing.T) {
	tests := []struct {
		name         string
		argv         []string
		wantMan      bool
		wantAgent    string
		wantInput    string
		wantErr      bool
		errSubstring string
	}{
		{name: "bare", argv: []string{"--man"}, wantMan: true},
		{name: "with_agent", argv: []string{"--man=echo"}, wantMan: true, wantAgent: "echo"},
		{name: "empty_value", argv: []string{"--man="}, wantErr: true, errSubstring: "missing value"},
		{name: "never_consumes_next", argv: []string{"--man", "prog.ann"}, wantMan: true, wantInput: "prog.ann"},
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
			if p.man != tc.wantMan {
				t.Errorf("man = %v, want %v", p.man, tc.wantMan)
			}
			if p.manAgent != tc.wantAgent {
				t.Errorf("manAgent = %q, want %q", p.manAgent, tc.wantAgent)
			}
			if p.input != tc.wantInput {
				t.Errorf("input = %q, want %q", p.input, tc.wantInput)
			}
		})
	}
}

func TestRunMan(t *testing.T) {
	t.Run("pool_exit_0", func(t *testing.T) {
		app, out, _ := newTestApp(t, &e2eSpawner{})
		code := app.Run([]string{"--man"})
		if code != 0 {
			t.Fatalf("--man exit = %d, want 0", code)
		}
		s := out.String()
		for _, want := range []string{"# [echo] — manual", "## Dispatch", "## Operation", "## Examples"} {
			if !strings.Contains(s, want) {
				t.Errorf("manual output missing %q:\n%s", want, s)
			}
		}
	})

	t.Run("single_agent_exit_0", func(t *testing.T) {
		app, out, _ := newTestApp(t, &e2eSpawner{})
		code := app.Run([]string{"--man=echo"})
		if code != 0 {
			t.Fatalf("--man=echo exit = %d, want 0", code)
		}
		if !strings.Contains(out.String(), "# [echo] — manual") {
			t.Errorf("single-agent manual missing [echo]:\n%s", out.String())
		}
	})

	t.Run("unknown_agent_exit_64", func(t *testing.T) {
		app, _, errb := newTestApp(t, &e2eSpawner{})
		code := app.Run([]string{"--man=nope"})
		if code != 64 {
			t.Fatalf("--man=nope exit = %d, want 64", code)
		}
		if !strings.Contains(errb.String(), "unknown agent") {
			t.Errorf("stderr missing 'unknown agent':\n%s", errb.String())
		}
	})

	t.Run("does_not_execute_program", func(t *testing.T) {
		sp := &e2eSpawner{}
		app, _, _ := newTestApp(t, sp)
		app.Run([]string{"--man", "prog.ann"})
		if sp.calls != 0 {
			t.Errorf("man should not dispatch any agent, calls = %d", sp.calls)
		}
	})
}

func TestHelpDocumentsMan(t *testing.T) {
	var b strings.Builder
	printHelp(&b)
	if !strings.Contains(b.String(), "--man") {
		t.Error("help does not document --man")
	}
}
