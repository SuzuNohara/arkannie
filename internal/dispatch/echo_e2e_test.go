package dispatch

import (
	"path/filepath"
	"strings"
	"testing"

	"arkannie/internal/ann"
	"arkannie/internal/ram"
	"arkannie/internal/registry"
)

// assemble runs the full flag→directive→prompt path for the real echo agent.
func assemble(t *testing.T, flags map[string]string, ctx string) string {
	t.Helper()
	reg, errs := registry.Load(filepath.Join("..", "..", ".agents"))
	if len(errs) > 0 {
		t.Fatalf("load .agents: %v", errs)
	}
	a, ok := reg.Resolve("echo")
	if !ok {
		t.Fatal("echo not registered")
	}
	op := a.Operations["echo"]
	d := &ann.Dispatch{Command: "echo", Flags: flags, Context: ctx}
	res, err := ResolveFlags(a, &op, "echo", d)
	if err != nil {
		t.Fatalf("ResolveFlags: %v", err)
	}
	cb, err := BuildContextBlock(&op, "echo", res.Data, ram.New())
	if err != nil {
		t.Fatalf("BuildContextBlock: %v", err)
	}
	pre, post := RenderDirectives(a, &op, res)
	return AssemblePrompt(a, cb, pre, post, "e1")
}

// TestEchoDirectivesE2E covers T10 (R14): the assembled prompt places group and
// personality directives before the context and modifiers after it.
func TestEchoDirectivesE2E(t *testing.T) {
	t.Run("all_directive_kinds_ordered", func(t *testing.T) {
		p := assemble(t, map[string]string{"backwards": "", "terse": "", "personality": "techlead"}, "hola")
		iDir := strings.Index(p, "#direction")
		iPers := strings.Index(p, "#personality")
		iCtx := strings.Index(p, "operation: echo")
		iMod := strings.Index(p, "#modifiers")
		if iDir < 0 || iPers < 0 || iCtx < 0 || iMod < 0 {
			t.Fatalf("missing a section: dir=%d pers=%d ctx=%d mod=%d\n%s", iDir, iPers, iCtx, iMod, p)
		}
		if !(iDir < iPers && iPers < iCtx && iCtx < iMod) {
			t.Errorf("wrong order dir=%d pers=%d ctx=%d mod=%d\n%s", iDir, iPers, iCtx, iMod, p)
		}
		for _, want := range []string{"reversed", "tech lead", "short as possible", "hola"} {
			if !strings.Contains(p, want) {
				t.Errorf("prompt missing %q", want)
			}
		}
	})

	t.Run("no_flags_default_personality_only", func(t *testing.T) {
		p := assemble(t, map[string]string{}, "plain")
		if strings.Contains(p, "#direction") || strings.Contains(p, "#modifiers") {
			t.Errorf("unexpected group/modifier tag with no flags:\n%s", p)
		}
		if !strings.Contains(p, "#personality") || !strings.Contains(p, "Answer plainly") {
			t.Errorf("default personality section missing:\n%s", p)
		}
	})
}
