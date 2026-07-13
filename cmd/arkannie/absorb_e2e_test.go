package main

// R11 wiring proof: the layer/workspace consent parsed from argv must travel
// to every BuildRunSpec call site. These tests drive the whole App over a
// layer agent fixture and assert the consequence — a layer dispatch is
// rejected (catchable) without --allow-layer and runs with cwd=origin when
// consent is granted — plus the argv→Consent mapping and the detach handoff.

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"arkannie/internal/spawn"
)

// recordedCwd returns the single spawn cwd the stub observed, failing if the
// program dispatched anything other than exactly once. Dispatch ids are
// auto-generated (s0, s1, …) for unlabeled dispatches, so tests assert on the
// value rather than a hard-coded key.
func recordedCwd(t *testing.T, sp *e2eSpawner) string {
	t.Helper()
	if len(sp.cwds) != 1 {
		t.Fatalf("want exactly one recorded dispatch cwd, got %v", sp.cwds)
	}
	for _, cwd := range sp.cwds {
		return cwd
	}
	return ""
}

// layerYAML is the echo contract marked as a layer over {{ORIGIN}}, which the
// test rewrites to a real directory holding a CLAUDE.md identity file.
const layerYAML = `command: "[nova]"
model: haiku
scope: agnostic
timeout: 60
default_operation: echo
capabilities:
  purpose: Echo the dispatched text from a layer origin.
  use_when: Only in tests.
layer:
  origin: "{{ORIGIN}}"
operations:
  echo:
    id: nova-op
    description: Return the text received in context.text unchanged.
    context:
      text:
        type: string
        required: false
    grants: [read]
    output_schema:
      success:
        echo: string
      error:
        reason: string
        recoverable: boolean
      info:
        message: string
`

// installLayerAgent writes a layer agent named "nova" whose origin is a fresh
// directory containing a CLAUDE.md, and returns that origin path. It replaces
// the echo agent from newTestApp so the program's [nova] dispatch resolves.
func installLayerAgent(t *testing.T, app *App) string {
	t.Helper()
	origin := t.TempDir()
	mustWrite(t, filepath.Join(origin, "CLAUDE.md"), "# Foreign AI identity\n")
	yaml := strings.ReplaceAll(layerYAML, "{{ORIGIN}}", origin)
	writeAgent(t, filepath.Join(app.Root, ".agents"), "nova", yaml)
	return origin
}

// writeLayerProg writes a one-line program dispatching [nova] and returns the
// path.
func writeLayerProg(t *testing.T, app *App) string {
	t.Helper()
	prog := filepath.Join(app.Root, "layer.ann")
	mustWrite(t, prog, "# ann v0.2\n[nova]: hola\n")
	return prog
}

func TestConsentFrom(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want spawn.Consent
	}{
		{"none", []string{"prog.ann"}, spawn.Consent{}},
		{"workspace_only", []string{"--allow-workspace", "prog.ann"}, spawn.Consent{Workspace: true}},
		{"layer_all", []string{"--allow-layer", "prog.ann"}, spawn.Consent{LayerAll: true}},
		{"layer_list", []string{"--allow-layer=nova,legacy", "prog.ann"},
			spawn.Consent{LayerList: []string{"nova", "legacy"}}},
		{"layer_partial", []string{"--allow-layer=nova", "prog.ann"},
			spawn.Consent{LayerList: []string{"nova"}}},
		{"workspace_and_layer", []string{"--allow-workspace", "--allow-layer=nova", "prog.ann"},
			spawn.Consent{Workspace: true, LayerList: []string{"nova"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := consentFrom(parseArgs(tc.argv))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("consentFrom(%v) = %+v, want %+v", tc.argv, got, tc.want)
			}
		})
	}
}

func TestE2EAbsorbLayer(t *testing.T) {
	t.Run("program_without_consent_is_blocked", func(t *testing.T) {
		sp := &e2eSpawner{cwds: map[string]string{}}
		app, out, _ := newTestApp(t, sp)
		installLayerAgent(t, app)
		prog := writeLayerProg(t, app)

		code := app.Run([]string{"--id=t", prog})
		if code != 1 {
			t.Fatalf("layer dispatch without --allow-layer should escalate (exit 1), got %d", code)
		}
		if sp.calls != 0 {
			t.Fatalf("claude must not be spawned when consent is missing, calls=%d", sp.calls)
		}
		body := readOutput(t, out.String())
		if !strings.Contains(body, "nova") || !strings.Contains(body, "--allow-layer") {
			t.Fatalf("error diagnostic should name the agent and the missing consent:\n%s", body)
		}
	})

	t.Run("program_with_consent_runs_in_origin", func(t *testing.T) {
		sp := &e2eSpawner{cwds: map[string]string{}}
		app, out, _ := newTestApp(t, sp)
		origin := installLayerAgent(t, app)
		prog := writeLayerProg(t, app)

		code := app.Run([]string{"--id=t", "--allow-layer", prog})
		if code != 0 {
			t.Fatalf("layer dispatch with --allow-layer should succeed, got %d", code)
		}
		if got := recordedCwd(t, sp); got != origin {
			t.Fatalf("spawn cwd = %q, want layer origin %q", got, origin)
		}
		if !strings.Contains(readOutput(t, out.String()), "status: success") {
			t.Fatalf("run should succeed")
		}
	})

	t.Run("whitelist_partial_matches_named_agent", func(t *testing.T) {
		sp := &e2eSpawner{cwds: map[string]string{}}
		app, _, _ := newTestApp(t, sp)
		origin := installLayerAgent(t, app)
		prog := writeLayerProg(t, app)

		code := app.Run([]string{"--id=t", "--allow-layer=nova", prog})
		if code != 0 {
			t.Fatalf("nova is whitelisted, run should succeed, got %d", code)
		}
		if got := recordedCwd(t, sp); got != origin {
			t.Fatalf("spawn cwd = %q, want origin %q", got, origin)
		}
	})

	t.Run("whitelist_non_match_is_blocked", func(t *testing.T) {
		sp := &e2eSpawner{cwds: map[string]string{}}
		app, _, _ := newTestApp(t, sp)
		installLayerAgent(t, app)
		prog := writeLayerProg(t, app)

		code := app.Run([]string{"--id=t", "--allow-layer=other", prog})
		if code != 1 {
			t.Fatalf("nova not in whitelist, run should escalate (exit 1), got %d", code)
		}
		if sp.calls != 0 {
			t.Fatalf("claude must not spawn for a non-whitelisted layer agent, calls=%d", sp.calls)
		}
	})

	t.Run("prompt_mode_carries_consent", func(t *testing.T) {
		sp := &e2eSpawner{cwds: map[string]string{}}
		app, _, _ := newTestApp(t, sp)
		origin := installLayerAgent(t, app)

		code := app.Run([]string{"--agent=nova", "--id=t", "--allow-layer", "hola"})
		if code != 0 {
			t.Fatalf("prompt mode with consent should succeed, got %d", code)
		}
		if got := recordedCwd(t, sp); got != origin {
			t.Fatalf("prompt-mode spawn cwd = %q, want origin %q", got, origin)
		}
	})

	t.Run("detach_preserves_allow_layer", func(t *testing.T) {
		sp := &e2eSpawner{cwds: map[string]string{}}
		app, _, _ := newTestApp(t, sp)
		installLayerAgent(t, app)
		prog := writeLayerProg(t, app)
		var forked []string
		app.ForkExec = func(args []string) error { forked = args; return nil }

		code := app.Run([]string{"--id=t", "--allow-layer=nova", "--detach", prog})
		if code != 0 {
			t.Fatalf("detach should return 0, got %d", code)
		}
		joined := strings.Join(forked, " ")
		if !strings.Contains(joined, "--allow-layer=nova") {
			t.Fatalf("detach child must preserve --allow-layer=nova: %v", forked)
		}
	})
}
