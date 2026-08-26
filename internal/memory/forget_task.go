package memory

import (
	"database/sql"
	"errors"
	"fmt"
)

var (
	// ErrTaskNotFound is returned when no execution exists for the requested task.
	ErrTaskNotFound = errors.New("task not found")
	// ErrTaskActive is returned instead of deleting a task that can still execute.
	ErrTaskActive = errors.New("task is still active")
)

// TaskSource identifies the adapter record associated with a persisted task.
// SourceIssueID is the adapter-native ID when available.
type TaskSource struct {
	Adapter       string
	SourceIssueID string
}

// GetTaskSource returns the source metadata of the latest execution for a task.
func (s *Store) GetTaskSource(taskID, projectPath string) (TaskSource, error) {
	if taskID == "" || projectPath == "" {
		return TaskSource{}, ErrTaskNotFound
	}

	canonicalPath := canonicalizeProjectPath(projectPath)
	var source TaskSource
	err := s.db.QueryRow(`
		SELECT task_source_adapter, task_source_issue_id
		FROM executions
		WHERE task_id = ? AND project_path IN (?, ?)
		ORDER BY created_at DESC
		LIMIT 1
	`, taskID, projectPath, canonicalPath).Scan(&source.Adapter, &source.SourceIssueID)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskSource{}, ErrTaskNotFound
	}
	if err != nil {
		return TaskSource{}, fmt.Errorf("get task source: %w", err)
	}
	return source, nil
}

// ForgetTask removes all persisted execution state for one task in one project.
// It is intended for an explicit operator reset, allowing a poller to treat a
// terminal task as unseen on its next pass. Active executions are never deleted:
// a running or queued worker could otherwise recreate partial state after reset.
func (s *Store) ForgetTask(taskID, projectPath string) error {
	if taskID == "" || projectPath == "" {
		return ErrTaskNotFound
	}

	canonicalPath := canonicalizeProjectPath(projectPath)
	return s.withRetry("ForgetTask", func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin forget task transaction: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		var total, active int
		err = tx.QueryRow(`
			SELECT COUNT(*), COALESCE(SUM(CASE WHEN status IN ('queued', 'pending', 'running', 'decomposed') THEN 1 ELSE 0 END), 0)
			FROM executions
			WHERE task_id = ? AND project_path IN (?, ?)
		`, taskID, projectPath, canonicalPath).Scan(&total, &active)
		if err != nil {
			return fmt.Errorf("find task executions: %w", err)
		}
		if total == 0 {
			return ErrTaskNotFound
		}
		if active > 0 {
			return ErrTaskActive
		}

		executionIDs := `SELECT id FROM executions WHERE task_id = ? AND project_path IN (?, ?)`
		for _, query := range []string{
			`DELETE FROM execution_events WHERE execution_id IN (` + executionIDs + `)`,
			`DELETE FROM execution_logs WHERE execution_id IN (` + executionIDs + `)`,
			`DELETE FROM usage_events WHERE execution_id IN (` + executionIDs + `)`,
			`DELETE FROM eval_tasks WHERE execution_id IN (` + executionIDs + `)`,
			`DELETE FROM pattern_feedback WHERE execution_id IN (` + executionIDs + `)`,
		} {
			if _, err := tx.Exec(query, taskID, projectPath, canonicalPath); err != nil {
				return fmt.Errorf("delete task execution data: %w", err)
			}
		}

		if _, err := tx.Exec(`DELETE FROM approval_pending WHERE task_id = ? AND project IN (?, ?, '')`, taskID, projectPath, canonicalPath); err != nil {
			return fmt.Errorf("delete task approvals: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM execution_claims WHERE task_id = ? AND project_path IN (?, ?)`, taskID, projectPath, canonicalPath); err != nil {
			return fmt.Errorf("delete task claims: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM repick_backoff WHERE key IN (?, ?)`, projectPath+"|"+taskID, canonicalPath+"|"+taskID); err != nil {
			return fmt.Errorf("delete task retry state: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM executions WHERE task_id = ? AND project_path IN (?, ?)`, taskID, projectPath, canonicalPath); err != nil {
			return fmt.Errorf("delete task executions: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit forget task transaction: %w", err)
		}
		return nil
	})
}
