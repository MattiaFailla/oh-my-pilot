package memory

import (
	"errors"
	"testing"
	"time"
)

func TestStoreForgetTask_RemovesOnlyRequestedTerminalTask(t *testing.T) {
	store := newForgetTaskTestStore(t)

	const taskID = "GH-42"
	const projectPath = "/tmp/forget-task"
	if err := store.SaveExecution(&Execution{ID: "forget-exec", TaskID: taskID, ProjectPath: projectPath, Status: "failed"}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}
	if err := store.SaveExecution(&Execution{ID: "other-exec", TaskID: taskID, ProjectPath: "/tmp/other-project", Status: "failed"}); err != nil {
		t.Fatalf("SaveExecution(other): %v", err)
	}
	if err := store.InsertExecutionEvent("forget-exec", StageFailed, "failed"); err != nil {
		t.Fatalf("InsertExecutionEvent: %v", err)
	}
	if _, err := store.DB().Exec(`INSERT INTO execution_logs (execution_id, level, message) VALUES ('forget-exec', 'info', 'log')`); err != nil {
		t.Fatalf("insert execution log: %v", err)
	}
	if _, err := store.DB().Exec(`INSERT INTO usage_events (id, user_id, project_id, event_type, execution_id) VALUES ('usage-1', 'user', 'project', 'execution', 'forget-exec')`); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}
	if _, err := store.DB().Exec(`INSERT INTO approval_pending (id, task_id, stage, title, project, expires_at) VALUES ('approval-1', ?, 'pre_execution', 'task', ?, CURRENT_TIMESTAMP)`, taskID, projectPath); err != nil {
		t.Fatalf("insert approval: %v", err)
	}
	if _, err := store.ClaimExecution(taskID, projectPath, 0, "forget-exec"); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if err := store.SetRepickBackoff(projectPath+"|"+taskID, 2, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("SetRepickBackoff: %v", err)
	}

	if err := store.ForgetTask(taskID, projectPath); err != nil {
		t.Fatalf("ForgetTask: %v", err)
	}
	if exists := executionExists(t, store, taskID, projectPath); exists {
		t.Error("forgotten task still exists")
	}
	if exists := executionExists(t, store, taskID, "/tmp/other-project"); !exists {
		t.Error("other project task was removed")
	}

	for _, query := range []string{
		`SELECT COUNT(*) FROM execution_events WHERE execution_id = 'forget-exec'`,
		`SELECT COUNT(*) FROM execution_logs WHERE execution_id = 'forget-exec'`,
		`SELECT COUNT(*) FROM usage_events WHERE execution_id = 'forget-exec'`,
		`SELECT COUNT(*) FROM approval_pending WHERE id = 'approval-1'`,
		`SELECT COUNT(*) FROM execution_claims WHERE task_id = 'GH-42' AND project_path = '/tmp/forget-task'`,
		`SELECT COUNT(*) FROM repick_backoff WHERE key = '/tmp/forget-task|GH-42'`,
	} {
		var count int
		if err := store.DB().QueryRow(query).Scan(&count); err != nil {
			t.Fatalf("query cleanup count: %v", err)
		}
		if count != 0 {
			t.Errorf("expected no rows after ForgetTask for query %q, got %d", query, count)
		}
	}
}

func TestStoreForgetTask_RefusesActiveTask(t *testing.T) {
	store := newForgetTaskTestStore(t)

	const taskID = "GH-active"
	const projectPath = "/tmp/active-task"
	if err := store.SaveExecution(&Execution{ID: "active-exec", TaskID: taskID, ProjectPath: projectPath, Status: "queued"}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	err := store.ForgetTask(taskID, projectPath)
	if !errors.Is(err, ErrTaskActive) {
		t.Fatalf("ForgetTask error = %v, want ErrTaskActive", err)
	}
	if exists := executionExists(t, store, taskID, projectPath); !exists {
		t.Error("active task was removed")
	}
}

func executionExists(t *testing.T, store *Store, taskID, projectPath string) bool {
	t.Helper()
	var exists bool
	if err := store.DB().QueryRow(`SELECT EXISTS(SELECT 1 FROM executions WHERE task_id = ? AND project_path = ?)`, taskID, projectPath).Scan(&exists); err != nil {
		t.Fatalf("query execution existence: %v", err)
	}
	return exists
}

func newForgetTaskTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
