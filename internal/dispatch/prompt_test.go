package dispatch

import (
	"os"
	"path/filepath"
	"testing"

	"arkannie/internal/registry"
)

const slotHarness = "You are wave {{ id }}.\n{{ directives_pre }}## Dispatch\n{{ context_block }}\n{{ directives_post }}End.\n"

func TestAssemblePrompt(t *testing.T) {
	a := &registry.Agent{Harness: slotHarness}

	t.Run("slots_filled_golden", func(t *testing.T) {
		got := AssemblePrompt(a, "operation: echo", "#direction\nrev\n", "#modifiers\nterse\n", "id-7")
		want := "You are wave id-7.\n#direction\nrev\n## Dispatch\noperation: echo\n#modifiers\nterse\nEnd.\n"
		if got != want {
			t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("empty_directives_no_phantom", func(t *testing.T) {
		got := AssemblePrompt(a, "cb", "", "", "id")
		want := "You are wave id.\n## Dispatch\ncb\nEnd.\n"
		if got != want {
			t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
		}
	})
}

// TestRenderDirectives covers T5 (R12).
func TestRenderDirectives(t *testing.T) {
	a := &registry.Agent{Personality: &registry.Personality{
		Default: "neutral",
		Values:  map[string]string{"techlead": "TL"},
	}}
	op := &registry.Operation{
		Groups: map[string]registry.Group{
			"direction": {"backwards": "REV"},
			"casing":    {"upper": "UP"},
		},
		Modifiers: map[string]string{"terse": "TERSE", "steps": "STEPS"},
	}

	t.Run("groups_personality_modifiers", func(t *testing.T) {
		res := &FlagResolution{
			Groups:      map[string]string{"direction": "backwards", "casing": "upper"},
			Personality: "techlead",
			Modifiers:   []string{"terse", "steps"},
		}
		pre, post := RenderDirectives(a, op, res)
		// Groups sorted: casing before direction.
		wantPre := "#casing\nUP\n#direction\nREV\n#personality\nTL\n"
		if pre != wantPre {
			t.Errorf("pre = %q, want %q", pre, wantPre)
		}
		// Modifiers sorted: steps before terse.
		wantPost := "#modifiers\nSTEPS\nTERSE\n"
		if post != wantPost {
			t.Errorf("post = %q, want %q", post, wantPost)
		}
	})

	t.Run("personality_default_no_modifiers", func(t *testing.T) {
		res := &FlagResolution{Groups: map[string]string{}}
		pre, post := RenderDirectives(a, op, res)
		if pre != "#personality\nneutral\n" {
			t.Errorf("pre = %q, want #personality/neutral", pre)
		}
		if post != "" {
			t.Errorf("post = %q, want empty", post)
		}
	})
}

func TestMaterializeRunDir(t *testing.T) {
	t.Run("U8-T3_run_dir_created_with_exact_prompt", func(t *testing.T) {
		memDir := t.TempDir()
		prompt := "You are wave rev-1.\n\noperation: echo\n\nEnd.\n"
		runDir, err := MaterializeRunDir(memDir, "20260702-045000", "rev-1", prompt)
		if err != nil {
			t.Fatalf("MaterializeRunDir: %v", err)
		}
		want := filepath.Join(memDir, "runs", "20260702-045000", "rev-1")
		if runDir != want {
			t.Fatalf("run dir: got %q, want %q", runDir, want)
		}
		b, err := os.ReadFile(filepath.Join(runDir, "prompt.md"))
		if err != nil {
			t.Fatalf("reading prompt.md: %v", err)
		}
		if string(b) != prompt {
			t.Fatalf("prompt.md content\n got: %q\nwant: %q", string(b), prompt)
		}
	})

	t.Run("U8-T4_dispatch_id_sanitized", func(t *testing.T) {
		memDir := t.TempDir()
		runDir, err := MaterializeRunDir(memDir, "run-1", "Seek Auth!", "p")
		if err != nil {
			t.Fatalf("MaterializeRunDir: %v", err)
		}
		if base := filepath.Base(runDir); base != "seek-auth-" {
			t.Fatalf("sanitized dir: got %q, want %q", base, "seek-auth-")
		}
		if _, err := os.Stat(filepath.Join(runDir, "prompt.md")); err != nil {
			t.Fatalf("prompt.md must exist in sanitized dir: %v", err)
		}
	})
}
