package main

// End-to-end, in-process exercise of the whole runtime: real App.Run over
// real .ann programs (testdata/e2e/*.ann) and prompt mode, driven by an
// intelligent Spawner stub that reads the rendered prompt, extracts the
// dispatch id and the context, and returns a well-formed trinary envelope.
// Every case asserts the persisted .output/<runID>.md and the exit code —
// this is the integration proof that parser, RAM, dispatch, spawner,
// envelope, scheduler, output and CLI fit together.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"

	"arkannie/internal/spawn"
)

// e2eSpawner simulates claude: it reads spec.PromptFile, extracts the dispatch
// id (the harness renders it as id `<id>`) and the rendered context, then
// returns a success envelope echoing the context. Per-id replies override the
// default to inject error/info envelopes. Invocation order and concurrency are
// recorded thread-safely for the parallel assertions.
type e2eSpawner struct {
	mu        sync.Mutex
	calls     int
	order     []string
	active    int
	maxActive int
	replies   map[string]string // id -> verbatim result YAML; absent = default echo
	cwds      map[string]string // id -> spec.Cwd observed; nil disables recording
}

func (s *e2eSpawner) Run(_ context.Context, spec spawn.RunSpec) (spawn.Result, error) {
	prompt, _ := os.ReadFile(spec.PromptFile)
	id := extractID(prompt)
	echo := flattenEcho(string(prompt))

	s.mu.Lock()
	s.calls++
	s.order = append(s.order, id)
	if s.cwds != nil {
		s.cwds[id] = spec.Cwd
	}
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	reply, custom := "", false
	if s.replies != nil {
		reply, custom = s.replies[id]
	}
	s.active--
	s.mu.Unlock()

	result := reply
	if !custom {
		result = defaultEcho(id, echo)
	}
	out, _ := json.Marshal(map[string]string{"result": result})
	return spawn.Result{Stdout: out}, nil
}

// extractID pulls the dispatch id from the rendered prompt. idRe (defined in
// main_test.go) matches the harness marker id `<id>`.
func extractID(prompt []byte) string {
	if m := idRe.FindSubmatch(prompt); m != nil {
		return string(m[1])
	}
	return ""
}

// defaultEcho builds a valid success envelope whose payload echoes the context.
func defaultEcho(id, echo string) string {
	b, _ := yaml.Marshal(map[string]any{
		"id":      id,
		"status":  "success",
		"payload": map[string]any{"echo": echo},
	})
	return string(b)
}

// flattenEcho parses the YAML context_block embedded in the prompt and returns
// every string leaf of its context map joined by spaces. This makes the stub
// "smart": a chained dispatch that received a prior payload as a context field
// echoes that payload's values back too, so the E2E-seq propagation is visible.
func flattenEcho(prompt string) string {
	start := strings.Index(prompt, "operation:")
	if start < 0 {
		return ""
	}
	region := prompt[start:]
	if end := strings.Index(region, "\nReturn"); end >= 0 {
		region = region[:end]
	}
	var block map[string]any
	if err := yaml.Unmarshal([]byte(region), &block); err != nil {
		return ""
	}
	ctx, _ := block["context"].(map[string]any)
	var leaves []string
	collectStrings(ctx, &leaves)
	return strings.Join(leaves, " ")
}

// collectStrings appends every string leaf reachable from v, visiting map keys
// in sorted order for deterministic output.
func collectStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			collectStrings(x[k], out)
		}
	case []any:
		for _, e := range x {
			collectStrings(e, out)
		}
	}
}

// errorReply is a valid error envelope (payload.reason + payload.recoverable).
func errorReply(id, reason string) string {
	return fmt.Sprintf("id: %s\nstatus: error\npayload:\n  reason: %s\n  recoverable: false\n", id, reason)
}

// infoReply is a valid info envelope carrying an Ask-Protocol missing_field.
func infoReply(id, message string) string {
	return fmt.Sprintf("id: %s\nstatus: info\npayload:\n  message: %s\n  missing_field: type\n  resumable: true\n",
		id, message)
}

func programPath(name string) string {
	return filepath.Join("testdata", "e2e", name)
}

// ---------------------------------------------------------------------------
// T-33 — E2E in-process suite
// ---------------------------------------------------------------------------

func TestE2E(t *testing.T) {
	t.Run("E2E-seq", func(t *testing.T) {
		sp := &e2eSpawner{}
		app, out, _ := newTestApp(t, sp)
		code := app.Run([]string{"--agent=echo", "--id=t", programPath("seq.ann")})
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
		if sp.calls != 2 {
			t.Fatalf("seq should dispatch twice, got %d", sp.calls)
		}
		body := readOutput(t, out.String())
		if !strings.Contains(body, "status: success") {
			t.Fatalf("seq run should succeed:\n%s", body)
		}
		// The program returns the chained binding: its echo must carry the first
		// dispatch's payload ("hola"), proving the binding chained.
		if !strings.Contains(body, "## chained") || !strings.Contains(body, "hola") {
			t.Fatalf("returned block should echo the first dispatch's payload:\n%s", body)
		}
	})

	t.Run("E2E-parallel", func(t *testing.T) {
		sp := &e2eSpawner{}
		app, out, _ := newTestApp(t, sp)
		code := app.Run([]string{"--agent=echo", "--id=t", programPath("parallel.ann")})
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
		if sp.calls != 2 {
			t.Fatalf("parallel should dispatch exactly twice, got %d", sp.calls)
		}
		gotA, gotB := false, false
		for _, id := range sp.order {
			gotA = gotA || id == "a"
			gotB = gotB || id == "b"
		}
		if !gotA || !gotB {
			t.Fatalf("both parallel ids should have run, saw %v", sp.order)
		}
		if !strings.Contains(readOutput(t, out.String()), "status: success") {
			t.Fatalf("parallel run should succeed")
		}
	})

	t.Run("E2E-error", func(t *testing.T) {
		sp := &e2eSpawner{replies: map[string]string{
			"fail": errorReply("fail", "the echo agent refused the boom request"),
		}}
		app, out, _ := newTestApp(t, sp)
		code := app.Run([]string{"--agent=echo", "--id=t", programPath("error.ann")})
		if code != 1 {
			t.Fatalf("unhandled wave error should exit 1, got %d", code)
		}
		body := readOutput(t, out.String())
		if !strings.Contains(body, "status: error") {
			t.Fatalf("error run should persist status error:\n%s", body)
		}
		// §8 diagnostic: header + Context block + the wave's reason.
		if !strings.Contains(body, "[arkannie] ERROR") || !strings.Contains(body, "refused the boom request") {
			t.Fatalf("output should carry the §8 error diagnostic:\n%s", body)
		}
	})

	t.Run("E2E-info", func(t *testing.T) {
		question := "Cual es el tipo de actividad (simple o project)"
		sp := &e2eSpawner{replies: map[string]string{
			"q": infoReply("q", question),
		}}
		app, out, _ := newTestApp(t, sp)
		code := app.Run([]string{"--agent=echo", "--id=t", programPath("info.ann")})
		if code != 2 {
			t.Fatalf("info with missing_field should exit 2, got %d", code)
		}
		body := readOutput(t, out.String())
		if !strings.Contains(body, "status: info") {
			t.Fatalf("info run should persist status info:\n%s", body)
		}
		if !strings.Contains(body, question) {
			t.Fatalf("the agent's question should reach the output:\n%s", body)
		}
	})

	t.Run("E2E-prompt", func(t *testing.T) {
		sp := &e2eSpawner{}
		app, out, _ := newTestApp(t, sp)
		code := app.Run([]string{"--agent=echo", "--id=t", "hola mundo"})
		if code != 0 {
			t.Fatalf("prompt mode should exit 0, got %d", code)
		}
		if sp.calls != 1 {
			t.Fatalf("prompt mode should dispatch exactly once, got %d", sp.calls)
		}
		path := strings.TrimSpace(out.String())
		if !strings.Contains(filepath.Base(path), "t") {
			t.Fatalf("runID should carry the --id label: %q", path)
		}
		body := readOutput(t, path)
		if !strings.Contains(body, "status: success") || !strings.Contains(body, "hola mundo") {
			t.Fatalf("prompt echo should reach the output verbatim:\n%s", body)
		}
	})

	t.Run("E2E-detach", func(t *testing.T) {
		sp := &e2eSpawner{}
		app, out, _ := newTestApp(t, sp)
		var forked []string
		app.ForkExec = func(args []string) error { forked = args; return nil }
		code := app.Run([]string{"--agent=echo", "--id=asyncjob", "--detach", "async work"})
		if code != 0 {
			t.Fatalf("detach should return 0 immediately, got %d", code)
		}
		path := strings.TrimSpace(out.String())
		want := filepath.Join(app.Root, ".output", "asyncjob.md")
		if path != want {
			t.Fatalf("detach should print the id-based output path, got %q want %q", path, want)
		}
		joined := strings.Join(forked, " ")
		if strings.Contains(joined, "--detach") {
			t.Fatalf("child args must not carry --detach: %v", forked)
		}
		if !strings.Contains(joined, "--_runid=") {
			t.Fatalf("child args must force the internal runID: %v", forked)
		}
	})
}
