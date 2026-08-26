package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/qf-studio/pilot/internal/autopilot"
	"github.com/qf-studio/pilot/internal/config"
	"github.com/qf-studio/pilot/internal/dashboard"
	"github.com/qf-studio/pilot/internal/executor"
	"github.com/qf-studio/pilot/internal/logging"
	"github.com/qf-studio/pilot/internal/memory"
)

func newDashboardTaskForgetHandler(
	store *memory.Store,
	monitor *executor.Monitor,
	cfg *config.Config,
	stateStore *autopilot.StateStore,
	clearGitHubProcessed func(repo string, issueNumber int),
) dashboard.TaskForgetHandler {
	return func(task dashboard.TaskDisplay) error {
		if store == nil {
			return fmt.Errorf("task reset is unavailable: execution store is not configured")
		}

		source, err := store.GetTaskSource(task.ID, task.ProjectPath)
		if err != nil {
			return err
		}
		if err := store.ForgetTask(task.ID, task.ProjectPath); err != nil {
			return err
		}

		adapter, repo, issueID := resetAdapterIdentity(task, source, cfg)
		if stateStore != nil && adapter != "" && issueID != "" {
			if err := stateStore.Unmark(adapter, repo, issueID); err != nil {
				logging.WithComponent("dashboard").Warn("task reset could not clear processed marker",
					"task_id", task.ID,
					"adapter", adapter,
					"issue_id", issueID,
					"error", err,
				)
			}
		}
		if clearGitHubProcessed != nil && adapter == "github" {
			if number, ok := githubIssueNumber(issueID); ok {
				clearGitHubProcessed(repo, number)
			}
		}
		if monitor != nil {
			monitor.Remove(task.ID)
		}

		logging.WithComponent("dashboard").Info("forgot task at operator request",
			"task_id", task.ID,
			"project_path", task.ProjectPath,
			"adapter", adapter,
		)
		return nil
	}
}

func resetAdapterIdentity(task dashboard.TaskDisplay, source memory.TaskSource, cfg *config.Config) (adapter, repo, issueID string) {
	adapter = strings.ToLower(source.Adapter)
	issueID = source.SourceIssueID
	if issueID == "" {
		issueID = task.ID
	}
	if adapter == "" {
		if _, ok := githubIssueNumber(task.ID); ok {
			adapter = "github"
		}
	}
	if adapter != "github" {
		return adapter, "", issueID
	}
	if number, ok := githubIssueNumber(issueID); ok {
		issueID = strconv.Itoa(number)
	}
	return adapter, githubRepoForTaskProject(cfg, task.ProjectPath), issueID
}

func githubIssueNumber(issueID string) (int, bool) {
	issueID = strings.TrimPrefix(issueID, "GH-")
	number, err := strconv.Atoi(issueID)
	return number, err == nil && number > 0
}

func githubRepoForTaskProject(cfg *config.Config, projectPath string) string {
	if cfg == nil {
		return ""
	}
	if project := cfg.FindProjectByPath(projectPath); project != nil && project.GitHub != nil &&
		project.GitHub.Owner != "" && project.GitHub.Repo != "" {
		return project.GitHub.Owner + "/" + project.GitHub.Repo
	}
	if cfg.Adapters != nil && cfg.Adapters.GitHub != nil {
		return cfg.Adapters.GitHub.Repo
	}
	return ""
}
