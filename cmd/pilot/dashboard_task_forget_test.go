package main

import (
	"testing"

	"github.com/qf-studio/pilot/internal/autopilot"
	"github.com/qf-studio/pilot/internal/config"
	"github.com/qf-studio/pilot/internal/dashboard"
	"github.com/qf-studio/pilot/internal/executor"
	"github.com/qf-studio/pilot/internal/memory"
)

func TestDashboardTaskForgetHandler_ClearsTaskAndGitHubMarkers(t *testing.T) {
	store, err := memory.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	const projectPath = "/workspace/app"
	if err := store.SaveExecution(&memory.Execution{
		ID:                "execution-42",
		TaskID:            "GH-42",
		ProjectPath:       projectPath,
		Status:            "failed",
		TaskSourceAdapter: "github",
		TaskSourceIssueID: "42",
	}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}
	stateStore, err := autopilot.NewStateStore(store.DB())
	if err != nil {
		t.Fatalf("autopilot.NewStateStore: %v", err)
	}
	if err := stateStore.Mark("github", "acme/app", "42"); err != nil {
		t.Fatalf("Mark: %v", err)
	}

	monitor := executor.NewMonitor()
	monitor.Register("GH-42", "Retry me", "")
	var clearedRepo string
	var clearedNumber int
	handler := newDashboardTaskForgetHandler(
		store,
		monitor,
		&config.Config{Projects: []*config.ProjectConfig{{
			Path:   projectPath,
			GitHub: &config.ProjectGitHubConfig{Owner: "acme", Repo: "app"},
		}}},
		stateStore,
		func(repo string, issueNumber int) {
			clearedRepo = repo
			clearedNumber = issueNumber
		},
	)

	if err := handler(dashboard.TaskDisplay{ID: "GH-42", ProjectPath: projectPath}); err != nil {
		t.Fatalf("handler: %v", err)
	}

	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM executions WHERE id = 'execution-42'`).Scan(&count); err != nil {
		t.Fatalf("query execution: %v", err)
	}
	if count != 0 {
		t.Errorf("executions count = %d, want 0", count)
	}
	if _, ok := monitor.Get("GH-42"); ok {
		t.Error("monitor still has forgotten task")
	}
	processed, err := stateStore.IsProcessed("github", "acme/app", "42")
	if err != nil {
		t.Fatalf("IsProcessed: %v", err)
	}
	if processed {
		t.Error("GitHub processed marker still exists")
	}
	if clearedRepo != "acme/app" || clearedNumber != 42 {
		t.Errorf("cleared GitHub marker = %q #%d, want acme/app #42", clearedRepo, clearedNumber)
	}
}
