package executor

import (
	"strings"
	"testing"
)

// GH-2432: "Opus plans, Sonnet executes" — verify the Sonnet/Opus split,
// AllowedTools/MCPConfig wiring, and Stop-hook default flip.

func TestDefaultBackendConfig_LeavesModelRoutingToOMPProfile(t *testing.T) {
	cfg := DefaultBackendConfig()
	if cfg.ModelRouting.Enabled {
		t.Error("ModelRouting should be disabled by default so the OMP profile selects models")
	}
}

func TestDefaultPlanningConfig_UsesOMPProfileModel(t *testing.T) {
	cfg := DefaultPlanningConfig()
	if cfg.Model != "" {
		t.Errorf("Planning.Model default = %q, want empty profile-selected model", cfg.Model)
	}
}

func TestDefaultBackendConfig_PlanningWired(t *testing.T) {
	cfg := DefaultBackendConfig()
	if cfg.Planning == nil {
		t.Fatal("DefaultBackendConfig() should populate Planning (GH-2432)")
	}
	if cfg.Planning.Model != "" {
		t.Errorf("DefaultBackendConfig.Planning.Model = %q, want empty profile-selected model", cfg.Planning.Model)
	}
}

func TestDefaultOMPConfig(t *testing.T) {
	cfg := DefaultBackendConfig()
	if cfg.OMP == nil {
		t.Fatal("DefaultBackendConfig.OMP is nil")
	}
	if cfg.OMP.Command != "omp" {
		t.Errorf("OMP.Command = %q, want omp", cfg.OMP.Command)
	}
}

func TestDefaultAllowedTools_PlanningIsReadOnly(t *testing.T) {
	tools := DefaultAllowedToolsPlanning()
	for _, banned := range []string{"Write", "Edit", "Bash"} {
		for _, tool := range tools {
			if tool == banned {
				t.Errorf("planning tools contain %q — planning must be read-only", banned)
			}
		}
	}
	for _, required := range []string{"Read", "Grep", "Glob"} {
		found := false
		for _, tool := range tools {
			if tool == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("planning tools missing required tool %q", required)
		}
	}
}

func TestExecuteOptions_HasAllowedToolsAndMCPConfigPath(t *testing.T) {
	// Compile-time check via field access.
	opts := ExecuteOptions{
		AllowedTools:  []string{"Read", "Bash"},
		MCPConfigPath: "/tmp/mcp.json",
	}
	if len(opts.AllowedTools) != 2 || opts.MCPConfigPath != "/tmp/mcp.json" {
		t.Error("ExecuteOptions.AllowedTools/MCPConfigPath not wired (GH-2432)")
	}
}

func TestRunner_ExecutionToolOptions_UsesOMPHostTools(t *testing.T) {
	r := &Runner{
		config: &BackendConfig{OMP: &OMPConfig{Command: "omp"}},
	}
	allowed, mcp := r.executionToolOptions()
	if allowed != nil {
		t.Errorf("allowed = %v, want nil", allowed)
	}
	if mcp != "" {
		t.Errorf("mcp = %q, want empty", mcp)
	}
}

func TestRunner_ExecutionToolOptions_NilSafe(t *testing.T) {
	r := &Runner{config: nil}
	allowed, mcp := r.executionToolOptions()
	if allowed != nil || mcp != "" {
		t.Errorf("nil config: allowed=%v mcp=%q, want nil/empty", allowed, mcp)
	}
}

func TestDefaultHooksConfig_RunTestsOnStop_FlippedToFalse(t *testing.T) {
	cfg := DefaultHooksConfig()
	if cfg.RunTestsOnStop == nil {
		t.Fatal("RunTestsOnStop is nil")
	}
	if *cfg.RunTestsOnStop {
		t.Error("RunTestsOnStop should default to false (GH-2432: cut subprocess token spend)")
	}
}

// Verify the planning tools list contains exactly what we ship by default —
// guards against accidental write-tool additions.
func TestDefaultAllowedToolsPlanning_Exact(t *testing.T) {
	got := strings.Join(DefaultAllowedToolsPlanning(), ",")
	want := "Read,Grep,Glob"
	if got != want {
		t.Errorf("planning tools = %q, want %q", got, want)
	}
}
