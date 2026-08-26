package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qf-studio/pilot/internal/adapterhealth"
	"github.com/qf-studio/pilot/internal/alerts"
	"github.com/qf-studio/pilot/internal/autopilot"
	"github.com/qf-studio/pilot/internal/budget"
	"github.com/qf-studio/pilot/internal/config"
	"github.com/qf-studio/pilot/internal/dashboard"
	"github.com/qf-studio/pilot/internal/executor"
	"github.com/qf-studio/pilot/internal/logging"
	"github.com/qf-studio/pilot/internal/memory"
)

// PollerDeps groups shared infrastructure used by all adapter poller startup blocks.
type PollerDeps struct {
	Cfg         *config.Config
	ProjectPath string

	Dispatcher   *executor.Dispatcher
	Runner       *executor.Runner
	Monitor      *executor.Monitor
	Program      *tea.Program
	AlertsEngine *alerts.Engine
	Enforcer     *budget.Enforcer
	Store        *memory.Store

	AutopilotController  *autopilot.Controller
	AutopilotStateStore  *autopilot.StateStore
	AutopilotControllers map[string]*autopilot.Controller // polling mode: per-repo controllers

	// GitHubPollers is the repo-keyed registry the SDK poller adds itself to so the
	// main.go sub-issue-skip / done-remark / stale-label loops can reach it — its
	// handle otherwise never leaves githubPollerRegistration() (GH-4110). Nil when
	// GitHub polling is off.
	GitHubPollers *githubPollerRegistry

	// AdapterHealth tracks panic/restart/disabled state for every adapter
	// goroutine started via SafeAdapterGo (GH-4314). Nil is tolerated —
	// SafeAdapterGo falls back to plain logging.SafeGo with no restart/health
	// tracking so callers that don't wire it (e.g. tests) still work.
	AdapterHealth *adapterhealth.Registry
}

// PollerRegistration describes a single adapter poller that can be conditionally started.
type PollerRegistration struct {
	Name           string
	Enabled        func(cfg *config.Config) bool
	CreateAndStart func(ctx context.Context, deps *PollerDeps)
}

// adapterPollerRegistrations returns the standard set of adapter poller registrations.
// The github registration (M7 4b/4d.2b) fans out one SDK poller per GitHub repo — the
// default adapter repo plus every projects[] entry; the in-tree fallback poller has
// been removed (GH-4170), so GitHub polling is SDK-only.
func adapterPollerRegistrations() []PollerRegistration {
	return []PollerRegistration{
		linearPollerRegistration(),
		jiraPollerRegistration(),
		asanaPollerRegistration(),
		azuredevopsPollerRegistration(),
		planePollerRegistration(),
		discordPollerRegistration(),
		gitlabPollerRegistration(),
		githubPollerRegistration(),
	}
}

// StartAdapterPollers iterates registrations and starts each enabled poller.
func StartAdapterPollers(ctx context.Context, deps *PollerDeps, registrations []PollerRegistration) {
	for _, reg := range registrations {
		if reg.Enabled(deps.Cfg) {
			logging.WithComponent("start").Info("Starting adapter poller",
				slog.String("adapter", reg.Name),
			)
			reg.CreateAndStart(ctx, deps)
		}
	}
}

// startPollActivityLog emits an immediate and recurring INFO record for a
// service's configured polling cadence. SDK pollers own their fetch loops, so
// this host-level heartbeat makes each scheduled poll visible without coupling
// the daemon to adapter-specific poller internals.
func startPollActivityLog(ctx context.Context, deps *PollerDeps, adapterName, service string, interval time.Duration, logger *slog.Logger, attrs ...slog.Attr) {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	logPollActivity(ctx, logger, service, interval, attrs...)
	publishPollActivity(deps.Program, service, interval, attrs...)
	deps.SafeAdapterGo(ctx, adapterName+":poll-activity", func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				logPollActivity(ctx, logger, service, interval, attrs...)
				publishPollActivity(deps.Program, service, interval, attrs...)
			}
		}
	})
}

func logPollActivity(ctx context.Context, logger *slog.Logger, service string, interval time.Duration, attrs ...slog.Attr) {
	fields := make([]slog.Attr, 0, len(attrs)+2)
	fields = append(fields, slog.String("service", service), slog.Duration("interval", interval))
	fields = append(fields, attrs...)
	logger.LogAttrs(ctx, slog.LevelInfo, "Polling service for new work", fields...)
}

func publishPollActivity(program *tea.Program, service string, interval time.Duration, attrs ...slog.Attr) {
	if program == nil {
		return
	}

	message := pollActivityDashboardMessage(service, interval, attrs...)
	go func() {
		program.Send(dashboard.AddLog(message)())
	}()
}

func pollActivityDashboardMessage(service string, interval time.Duration, attrs ...slog.Attr) string {
	target := ""
	for _, attr := range attrs {
		if attr.Key == "repo" || attr.Key == "project" {
			target = attr.Value.String()
			if target != "" {
				break
			}
		}
	}

	if target == "" {
		return fmt.Sprintf("◌ polling %s · every %s", service, interval)
	}
	return fmt.Sprintf("◌ polling %s · %s · every %s", service, target, interval)
}

// SafeAdapterGo is the single entry point every adapter poller goroutine
// must use to launch its listen loop (GH-4314): a panic in one adapter
// (e.g. the studio-sdk Discord gateway's nil-conn deref that took down the
// whole daemon) is recovered, logged with the adapter name, and the
// goroutine is restarted with backoff instead of crashing the process. Core
// executor/dispatcher goroutines must NOT go through this path — their
// panics indicate real corruption and should keep crashing loudly.
func (d *PollerDeps) SafeAdapterGo(ctx context.Context, name string, fn func()) {
	if d.AdapterHealth == nil {
		logging.SafeGo(name, fn)
		return
	}
	d.AdapterHealth.Go(ctx, name, d.alertAdapterPanic, fn)
}

// alertAdapterPanic raises a WARN-level service_unhealthy alert when an
// adapter goroutine panics — an adapter going down is an ops signal, not a
// silent degrade. Mirrors the config_error alerting pattern already used
// for adapter credential failures in adapter_preflight.go.
func (d *PollerDeps) alertAdapterPanic(name, message string) {
	if d.AlertsEngine == nil {
		return
	}
	d.AlertsEngine.ProcessEvent(alerts.Event{
		Type:      alerts.EventTypeConfigError,
		Error:     message,
		Metadata:  map[string]string{"adapter": name},
		Timestamp: time.Now(),
	})
}
