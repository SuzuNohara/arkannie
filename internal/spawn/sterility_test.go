package spawn

import (
	"strings"
	"testing"

	"arkannie/internal/registry"
)

// TestSterilityBelt — RT-01 / ADR-015. Every wave agent, whatever its scope,
// must be dispatched with the delegation-capable tools denied: SPK-spawn-
// sterility (2026-10-18) measured that without an explicit --disallowedTools
// a `claude -p` with only --allowedTools Read still invokes Agent and the
// sub-agent runs Bash. The belt is scope-independent and never leaks into
// AllowedTools.
func TestSterilityBelt(t *testing.T) {
	layerAgent := testAgent("agnostic", 0)
	layerAgent.Layer = &registry.Layer{Origin: t.TempDir()}

	cases := []struct {
		name    string
		agent   *registry.Agent
		op      *registry.Operation
		consent Consent
	}{
		{"agnostic", testAgent("agnostic", 0), testOp("read"), Consent{}},
		{"agnostic_network", testAgent("agnostic", 0), testOp("read", "network"), Consent{}},
		{"executor", testAgent("executor", 0), testOp("read", "write", "execute"), Consent{Workspace: true}},
		{"layer", layerAgent, testOp("read"), Consent{LayerAll: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := BuildRunSpec(tc.agent, tc.op, "/run/prompt.md", t.TempDir(), t.TempDir(), tc.consent, 0, testCfg())
			if err != nil {
				t.Fatalf("BuildRunSpec: %v", err)
			}
			dis := " " + strings.Join(spec.DisallowedTools, " ") + " "
			allowed := " " + strings.Join(spec.AllowedTools, " ") + " "
			for _, tool := range sterilityBelt {
				if !strings.Contains(dis, " "+tool+" ") {
					t.Errorf("%s: DisallowedTools %v lacks %s", tc.name, spec.DisallowedTools, tool)
				}
				if strings.Contains(allowed, " "+tool+" ") {
					t.Errorf("%s: AllowedTools %v must never contain %s", tc.name, spec.AllowedTools, tool)
				}
			}
			seen := map[string]int{}
			for _, tool := range spec.DisallowedTools {
				seen[tool]++
				if seen[tool] > 1 {
					t.Errorf("%s: DisallowedTools has duplicate %s", tc.name, tool)
				}
			}
		})
	}

	t.Run("belt_is_idempotent_and_keeps_order", func(t *testing.T) {
		in := []string{"Write", "Agent", "Edit"}
		got := withSterilityBelt(withSterilityBelt(in))
		if got[0] != "Write" || got[1] != "Agent" || got[2] != "Edit" {
			t.Errorf("order changed: %v", got)
		}
		if len(got) != 3+len(sterilityBelt)-1 {
			t.Errorf("len = %d, want %d (Agent deduplicated)", len(got), 3+len(sterilityBelt)-1)
		}
	})
}
