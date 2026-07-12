package spawn

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"arkannie/internal/config"
	"arkannie/internal/registry"
)

func TestReadOnlyRetryTooling(t *testing.T) {
	t.Run("WithoutSideEffects strips write and execute tools", func(t *testing.T) {
		got := WithoutSideEffects([]string{"Read", "Grep", "Glob", "Write", "Edit", "Bash"})
		want := []string{"Read", "Grep", "Glob"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("WithoutSideEffects = %v, want %v", got, want)
		}
	})
	t.Run("PlusSideEffects merges the belt without duplicates", func(t *testing.T) {
		got := PlusSideEffects([]string{"WebFetch", "Bash"})
		joined := strings.Join(got, ",")
		for _, need := range []string{"WebFetch", "Write", "Edit", "Bash", "NotebookEdit"} {
			if !strings.Contains(joined, need) {
				t.Errorf("PlusSideEffects(%v) missing %s", got, need)
			}
		}
		if strings.Count(joined, "Bash") != 1 {
			t.Errorf("Bash duplicated in %v", got)
		}
	})
}

func testAgent(scope string, timeoutSecs int) *registry.Agent {
	return &registry.Agent{
		Command: "echo",
		Model:   "claude-sonnet-4-5",
		Scope:   scope,
		Timeout: timeoutSecs,
		Dir:     ".agents/echo/",
	}
}

// testLayerAgent builds an agent with a layer.origin marker. command carries
// its dispatch brackets so the whitelist bracket-strip is exercised.
func testLayerAgent(scope, command, origin string) *registry.Agent {
	return &registry.Agent{
		Command: command,
		Model:   "claude-sonnet-4-5",
		Scope:   scope,
		Layer:   &registry.Layer{Origin: origin},
		Dir:     ".agents/layer/",
	}
}

func testOp(grants ...string) *registry.Operation {
	return &registry.Operation{ID: "run", Grants: grants}
}

func testCfg() *config.Config {
	return &config.Config{
		TimeoutDefault: 120,
		MaxConcurrency: 4,
		ClaudeBin:      "claude",
	}
}

func TestBuildRunSpec(t *testing.T) {
	t.Run("U9-T1_agnostic", func(t *testing.T) {
		runDir := t.TempDir()
		invokerCwd := t.TempDir()
		spec, err := BuildRunSpec(testAgent("agnostic", 0), testOp("read"),
			"/run/prompt.md", runDir, invokerCwd, Consent{}, 0, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v", err)
		}
		if spec.Cwd != runDir {
			t.Errorf("Cwd = %q, want runDir %q", spec.Cwd, runDir)
		}
		wantAllowed := []string{"Read", "Grep", "Glob"}
		if !reflect.DeepEqual(spec.AllowedTools, wantAllowed) {
			t.Errorf("AllowedTools = %v, want %v", spec.AllowedTools, wantAllowed)
		}
		wantDisallowed := []string{"Write", "Edit", "Bash", "NotebookEdit"}
		if !reflect.DeepEqual(spec.DisallowedTools, wantDisallowed) {
			t.Errorf("DisallowedTools = %v, want %v", spec.DisallowedTools, wantDisallowed)
		}
		if len(spec.AddDirs) != 0 {
			t.Errorf("AddDirs = %v, want empty", spec.AddDirs)
		}
		if spec.Model != "claude-sonnet-4-5" {
			t.Errorf("Model = %q, want agent model", spec.Model)
		}
		if spec.PromptFile != "/run/prompt.md" {
			t.Errorf("PromptFile = %q, want /run/prompt.md", spec.PromptFile)
		}
	})

	t.Run("U9-T1_agnostic_read_network", func(t *testing.T) {
		runDir := t.TempDir()
		spec, err := BuildRunSpec(testAgent("agnostic", 0), testOp("read", "network"),
			"/run/prompt.md", runDir, t.TempDir(), Consent{}, 0, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v", err)
		}
		wantAllowed := []string{"Read", "Grep", "Glob", "WebFetch", "WebSearch"}
		if !reflect.DeepEqual(spec.AllowedTools, wantAllowed) {
			t.Errorf("AllowedTools = %v, want %v", spec.AllowedTools, wantAllowed)
		}
	})

	t.Run("U9-T2_executor_with_allow_workspace", func(t *testing.T) {
		runDir := t.TempDir()
		invokerCwd := t.TempDir()
		spec, err := BuildRunSpec(testAgent("executor", 0), testOp("read", "write", "execute"),
			"/run/prompt.md", runDir, invokerCwd, Consent{Workspace: true}, 0, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v", err)
		}
		if spec.Cwd != invokerCwd {
			t.Errorf("Cwd = %q, want invokerCwd %q", spec.Cwd, invokerCwd)
		}
		wantAllowed := []string{"Read", "Grep", "Glob", "Write", "Edit", "Bash"}
		if !reflect.DeepEqual(spec.AllowedTools, wantAllowed) {
			t.Errorf("AllowedTools = %v, want %v", spec.AllowedTools, wantAllowed)
		}
		wantDisallowed := []string{"WebFetch", "WebSearch"}
		if !reflect.DeepEqual(spec.DisallowedTools, wantDisallowed) {
			t.Errorf("DisallowedTools = %v, want complement %v", spec.DisallowedTools, wantDisallowed)
		}
	})

	t.Run("U9-T3_executor_without_allow_workspace", func(t *testing.T) {
		_, err := BuildRunSpec(testAgent("executor", 0), testOp("read", "write"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{}, 0, testCfg())
		if err == nil {
			t.Fatal("BuildRunSpec: want PreDispatchError, got nil")
		}
		var pde *PreDispatchError
		if !errors.As(err, &pde) {
			t.Fatalf("error type = %T, want *PreDispatchError", err)
		}
		if pde.Class != 'B' {
			t.Errorf("Class = %c, want B", pde.Class)
		}
	})

	t.Run("U9-T7_flag_beats_yaml_and_default", func(t *testing.T) {
		spec, err := BuildRunSpec(testAgent("agnostic", 60), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{}, 30, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v", err)
		}
		if spec.Timeout != 30*time.Second {
			t.Errorf("Timeout = %v, want 30s (flag wins)", spec.Timeout)
		}
	})

	t.Run("U9-T7_yaml_beats_default", func(t *testing.T) {
		spec, err := BuildRunSpec(testAgent("agnostic", 60), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{}, 0, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v", err)
		}
		if spec.Timeout != 60*time.Second {
			t.Errorf("Timeout = %v, want 60s (agent yaml wins)", spec.Timeout)
		}
	})

	t.Run("U9-T7_config_default", func(t *testing.T) {
		spec, err := BuildRunSpec(testAgent("agnostic", 0), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{}, 0, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v", err)
		}
		if spec.Timeout != 120*time.Second {
			t.Errorf("Timeout = %v, want 120s (config default)", spec.Timeout)
		}
	})

	t.Run("U9-T7_negative_flag_class_A", func(t *testing.T) {
		_, err := BuildRunSpec(testAgent("agnostic", 0), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{}, -1, testCfg())
		if err == nil {
			t.Fatal("BuildRunSpec: want PreDispatchError, got nil")
		}
		var pde *PreDispatchError
		if !errors.As(err, &pde) {
			t.Fatalf("error type = %T, want *PreDispatchError", err)
		}
		if pde.Class != 'A' {
			t.Errorf("Class = %c, want A", pde.Class)
		}
	})
}

// TestBuildRunSpecLayer covers R9/R10: layer agents spawn with cwd=origin,
// gated by a dedicated layer consent, with the belt still fixed by scope.
func TestBuildRunSpecLayer(t *testing.T) {
	origin := "/opt/legacy-ai"

	t.Run("T4.1_layer_without_consent_class_B_names_agent_and_origin", func(t *testing.T) {
		_, err := BuildRunSpec(testLayerAgent("agnostic", "[nova]", origin), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{}, 0, testCfg())
		if err == nil {
			t.Fatal("BuildRunSpec: want PreDispatchError, got nil")
		}
		var pde *PreDispatchError
		if !errors.As(err, &pde) {
			t.Fatalf("error type = %T, want *PreDispatchError", err)
		}
		if pde.Class != 'B' {
			t.Errorf("Class = %c, want B", pde.Class)
		}
		for _, want := range []string{"nova", origin, "--allow-layer"} {
			if !strings.Contains(pde.Msg, want) {
				t.Errorf("Msg = %q, missing %q", pde.Msg, want)
			}
		}
	})

	t.Run("T4.2_layer_all_consent_cwd_is_origin", func(t *testing.T) {
		spec, err := BuildRunSpec(testLayerAgent("agnostic", "[nova]", origin), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{LayerAll: true}, 0, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v", err)
		}
		if spec.Cwd != origin {
			t.Errorf("Cwd = %q, want origin %q", spec.Cwd, origin)
		}
	})

	t.Run("T4.3_whitelist_match_cwd_is_origin", func(t *testing.T) {
		spec, err := BuildRunSpec(testLayerAgent("agnostic", "[nova]", origin), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{LayerList: []string{"nova", "legacy"}}, 0, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v", err)
		}
		if spec.Cwd != origin {
			t.Errorf("Cwd = %q, want origin %q", spec.Cwd, origin)
		}
	})

	t.Run("T4.4_brackets_stripped_for_whitelist_match", func(t *testing.T) {
		// agent command "[nova]" must match the bare whitelist name "nova".
		spec, err := BuildRunSpec(testLayerAgent("agnostic", "[nova]", origin), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{LayerList: []string{"nova"}}, 0, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v (brackets not stripped?)", err)
		}
		if spec.Cwd != origin {
			t.Errorf("Cwd = %q, want origin %q", spec.Cwd, origin)
		}
	})

	t.Run("T4.6_whitelist_non_match_class_B", func(t *testing.T) {
		_, err := BuildRunSpec(testLayerAgent("agnostic", "[nova]", origin), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{LayerList: []string{"legacy"}}, 0, testCfg())
		if err == nil {
			t.Fatal("BuildRunSpec: want PreDispatchError, got nil")
		}
		var pde *PreDispatchError
		if !errors.As(err, &pde) || pde.Class != 'B' {
			t.Fatalf("error = %v, want Class B PreDispatchError", err)
		}
	})

	t.Run("T4.5_agnostic_layer_keeps_containment_belt", func(t *testing.T) {
		spec, err := BuildRunSpec(testLayerAgent("agnostic", "[nova]", origin), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{LayerAll: true}, 0, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v", err)
		}
		wantDisallowed := []string{"Write", "Edit", "Bash", "NotebookEdit"}
		if !reflect.DeepEqual(spec.DisallowedTools, wantDisallowed) {
			t.Errorf("DisallowedTools = %v, want agnostic belt %v", spec.DisallowedTools, wantDisallowed)
		}
	})

	t.Run("T4.8_executor_layer_grant_complement_no_workspace_needed", func(t *testing.T) {
		// A layer executor agent needs no --allow-workspace: layer consent is
		// strictly stronger. Belt is the grant complement, cwd is the origin.
		spec, err := BuildRunSpec(testLayerAgent("executor", "[nova]", origin), testOp("read", "write", "execute"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{LayerAll: true}, 0, testCfg())
		if err != nil {
			t.Fatalf("BuildRunSpec: unexpected error %v", err)
		}
		if spec.Cwd != origin {
			t.Errorf("Cwd = %q, want origin %q", spec.Cwd, origin)
		}
		wantDisallowed := []string{"WebFetch", "WebSearch"}
		if !reflect.DeepEqual(spec.DisallowedTools, wantDisallowed) {
			t.Errorf("DisallowedTools = %v, want grant complement %v", spec.DisallowedTools, wantDisallowed)
		}
	})

	t.Run("order_timeout_beats_consent", func(t *testing.T) {
		// Negative timeout is a Class A usage error and must win over the
		// Class B consent rejection even for a layer agent without consent.
		_, err := BuildRunSpec(testLayerAgent("agnostic", "[nova]", origin), testOp("read"),
			"/run/prompt.md", t.TempDir(), t.TempDir(), Consent{}, -1, testCfg())
		var pde *PreDispatchError
		if !errors.As(err, &pde) || pde.Class != 'A' {
			t.Fatalf("error = %v, want Class A (timeout beats consent)", err)
		}
	})
}

func TestPreDispatchErrorMessage(t *testing.T) {
	err := &PreDispatchError{Class: 'B', Msg: "workspace access denied"}
	got := err.Error()
	if got == "" {
		t.Fatal("Error() returned empty string")
	}
	for _, want := range []string{"B", "workspace access denied"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}
