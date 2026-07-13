package interpreter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"arkannie/internal/ann"
	"arkannie/internal/config"
	"arkannie/internal/envelope"
)

// recordingStub is an ExecFunc stub that records its invocation arguments and
// returns a preconfigured stdout/exitCode/err. No real claude is ever run.
type recordingStub struct {
	stdout   []byte
	exitCode int
	err      error

	gotBin  string
	gotArgs []string
	gotCwd  string
	called  int
}

func (s *recordingStub) exec(_ context.Context, bin string, args []string, cwd string) ([]byte, int, error) {
	s.called++
	s.gotBin = bin
	s.gotArgs = args
	s.gotCwd = cwd
	return s.stdout, s.exitCode, s.err
}

// claudeJSON marshals result into the {"result": ...} shape emitted by
// `claude -p --output-format json`.
func claudeJSON(t *testing.T, result string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"result": result})
	if err != nil {
		t.Fatalf("marshaling stub claude JSON: %v", err)
	}
	return b
}

func testConfig() *config.Config {
	return &config.Config{
		TimeoutDefault: 120,
		MaxConcurrency: 4,
		ClaudeBin:      "claude",
		Root:           "/arkannie",
	}
}

func testParseError() *ann.ParseError {
	return &ann.ParseError{Line: 4, Col: 2, Category: ann.Syntax, Msg: "unclosed block"}
}

func TestTryRepair(t *testing.T) {
	t.Run("U15-T2_ann_block_returns_fixed", func(t *testing.T) {
		program := "# ann v0.2\n[echo] --id greet --text \"hi\""
		result := "Here is the fix:\n```ann\n" + program + "\n```\nDone."
		stub := &recordingStub{stdout: claudeJSON(t, result)}

		fixed, giveUp, err := TryRepair(stub.exec, testConfig(), "/arkannie", []byte("broken"), testParseError())
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if giveUp != nil {
			t.Fatalf("expected giveUp nil, got %+v", giveUp)
		}
		if string(fixed) != program {
			t.Fatalf("fixed mismatch:\n got: %q\nwant: %q", string(fixed), program)
		}
	})

	t.Run("U15-T3_argv_and_prompt_shape", func(t *testing.T) {
		program := "# ann v0.2\n[echo] --id x"
		result := "```ann\n" + program + "\n```"
		stub := &recordingStub{stdout: claudeJSON(t, result)}
		perr := testParseError()

		_, _, err := TryRepair(stub.exec, testConfig(), "/arkannie", []byte("broken"), perr)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if stub.gotCwd != "/arkannie" {
			t.Fatalf("cwd = %q, want /arkannie", stub.gotCwd)
		}
		if !hasFlagValue(stub.gotArgs, "--model", "sonnet") {
			t.Fatalf("argv missing --model sonnet: %v", stub.gotArgs)
		}
		if !hasFlagValue(stub.gotArgs, "--add-dir", "/arkannie/spec") {
			t.Fatalf("argv missing --add-dir /arkannie/spec: %v", stub.gotArgs)
		}
		if !hasFlagValue(stub.gotArgs, "--output-format", "json") {
			t.Fatalf("argv missing --output-format json: %v", stub.gotArgs)
		}
		prompt := promptArg(stub.gotArgs)
		if !strings.Contains(prompt, "4") {
			t.Fatalf("prompt missing ParseError line 4: %q", prompt)
		}
		if !strings.Contains(prompt, "Syntax") {
			t.Fatalf("prompt missing ParseError category Syntax: %q", prompt)
		}
		if !strings.Contains(prompt, perr.Msg) {
			t.Fatalf("prompt missing ParseError message: %q", prompt)
		}
		if !strings.Contains(prompt, "broken") {
			t.Fatalf("prompt missing broken .ann source verbatim: %q", prompt)
		}
	})

	t.Run("U15-T4_giveup_returns_info_envelope", func(t *testing.T) {
		msg := "falta cerrar el bloque en la línea 4"
		result := "GIVEUP: " + msg
		stub := &recordingStub{stdout: claudeJSON(t, result)}

		fixed, giveUp, err := TryRepair(stub.exec, testConfig(), "/arkannie", []byte("broken"), testParseError())
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if fixed != nil {
			t.Fatalf("expected fixed nil, got %q", string(fixed))
		}
		if giveUp == nil {
			t.Fatal("expected giveUp envelope, got nil")
		}
		if giveUp.Status != envelope.StatusInfo {
			t.Fatalf("giveUp.Status = %q, want info", giveUp.Status)
		}
		gm, _ := giveUp.Payload.(map[string]any)
		got, _ := gm["message"].(string)
		if !strings.Contains(got, msg) {
			t.Fatalf("giveUp message = %q, want to contain %q", got, msg)
		}
	})

	t.Run("U15-T5_unintelligible_output_errors", func(t *testing.T) {
		stub := &recordingStub{stdout: claudeJSON(t, "I am not sure what to do here.")}
		fixed, giveUp, err := TryRepair(stub.exec, testConfig(), "/arkannie", []byte("broken"), testParseError())
		if err == nil {
			t.Fatal("expected err for unintelligible output, got nil")
		}
		if fixed != nil || giveUp != nil {
			t.Fatalf("expected fixed/giveUp nil on err, got fixed=%q giveUp=%+v", string(fixed), giveUp)
		}
	})

	t.Run("U15-T5_nonzero_exit_errors", func(t *testing.T) {
		stub := &recordingStub{stdout: claudeJSON(t, "```ann\nx\n```"), exitCode: 1}
		_, _, err := TryRepair(stub.exec, testConfig(), "/arkannie", []byte("broken"), testParseError())
		if err == nil {
			t.Fatal("expected err for exitCode=1, got nil")
		}
	})

	t.Run("invalid_json_stdout_errors", func(t *testing.T) {
		stub := &recordingStub{stdout: []byte("not json at all")}
		_, _, err := TryRepair(stub.exec, testConfig(), "/arkannie", []byte("broken"), testParseError())
		if err == nil {
			t.Fatal("expected err for non-JSON stdout, got nil")
		}
	})

	t.Run("exec_error_wrapped", func(t *testing.T) {
		stub := &recordingStub{err: errStub}
		_, _, err := TryRepair(stub.exec, testConfig(), "/arkannie", []byte("broken"), testParseError())
		if err == nil {
			t.Fatal("expected err for exec failure, got nil")
		}
	})

	t.Run("empty_giveup_is_unintelligible", func(t *testing.T) {
		stub := &recordingStub{stdout: claudeJSON(t, "GIVEUP:   ")}
		_, giveUp, err := TryRepair(stub.exec, testConfig(), "/arkannie", []byte("broken"), testParseError())
		if err == nil {
			t.Fatal("expected err for empty GIVEUP text, got nil")
		}
		if giveUp != nil {
			t.Fatalf("expected giveUp nil, got %+v", giveUp)
		}
	})

	t.Run("category_names_and_prompt_source", func(t *testing.T) {
		cases := []struct {
			cat  ann.Category
			want string
		}{
			{ann.Syntax, "Syntax"},
			{ann.UnknownCommand, "UnknownCommand"},
			{ann.Type, "Type"},
			{ann.VersionMismatch, "VersionMismatch"},
			{ann.Category(99), "Unknown"},
		}
		for _, tc := range cases {
			perr := &ann.ParseError{Line: 1, Col: 1, Category: tc.cat, Msg: "m"}
			p := buildRepairPrompt([]byte("src"), perr)
			if !strings.Contains(p, tc.want) {
				t.Fatalf("category %v: prompt missing %q: %q", tc.cat, tc.want, p)
			}
		}
	})

	t.Run("truncate_long_output", func(t *testing.T) {
		long := strings.Repeat("x", 500)
		stub := &recordingStub{stdout: claudeJSON(t, long)}
		_, _, err := TryRepair(stub.exec, testConfig(), "/arkannie", []byte("broken"), testParseError())
		if err == nil {
			t.Fatal("expected err for unintelligible long output, got nil")
		}
		if !strings.Contains(err.Error(), "…") {
			t.Fatalf("expected truncation ellipsis in err: %v", err)
		}
	})
}

var errStub = errors.New("stub exec failure")

// hasFlagValue reports whether args contains flag immediately followed by value.
func hasFlagValue(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// promptArg returns the value following the -p flag.
func promptArg(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-p" {
			return args[i+1]
		}
	}
	return ""
}
