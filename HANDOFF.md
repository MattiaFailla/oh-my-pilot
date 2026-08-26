# oh-my-pilot Handoff

Last updated: 2026-08-27

## Current status

The OMP migration is operational on macOS. `oh-my-pilot` now polls GitHub,
creates an isolated worktree, launches Oh My Pi, creates a PR, and supervises
CI without auto-merging. The local configuration validates successfully and
`oh-my-pilot doctor` reports ready to start.

## Local operating setup

- Repository: `/Users/fiscozen/w/oh-my-pilot`
- Remote: `https://github.com/MattiaFailla/oh-my-pilot.git`
- Branch: `main`
- Current shipped commit: `96daf62a fix: validate manual-merge autopilot config`
- Development binary: `bin/oh-my-pilot` (macOS arm64)
- Start command: `bin/oh-my-pilot start --dashboard --replace`
- Active config: `~/.oh-my-pilot/config.yaml`
- OMP executable: `/Users/fiscozen/.bun/bin/omp` (`omp/18.0.6`)
- OMP profile root: `~/.omp`; OMP RPC/session directory: `~/.omp/agent`

## Active configuration policy

- GitHub polling is enabled for `fiscozen/it.fiscozen.app` every 30 seconds.
- The trigger label is `oh-my-pilot`.
- The project checkout is `/Users/fiscozen/w/base/it-fiscozen-app` on `master`.
- Autopilot is enabled for CI and PR supervision with `auto_merge: false`.
- GitHub auth uses the locally authenticated `gh` CLI. The config intentionally
  leaves `adapters.github.token` empty, avoiding a mandatory `GITHUB_TOKEN`
  environment variable.
- The OMP pre-flight judge is enabled.

## Recent shipped work

- `96daf62a`: fixed configuration validation for `gh auth token` fallback and
  stopped `doctor` from treating inactive approval paths as fatal in manual-
  merge deployments.
- `12c77fc5`: propagated OMP profile paths, surfaced OMP provider errors,
  moved PR recovery to a background startup job, and added the rebuild-only
  post-commit hook.
- `a2e1a8e7`: added a dashboard action to reset/delete task state as if the
  system had never seen the task.
- `5ba4a3bb`: used the configured project base branch for worktree creation.
- `157edbee` and `f36bde9d`: added GitHub polling visibility in dashboard logs.

## Verification completed

- `bin/oh-my-pilot config validate` passes with no warnings.
- `bin/oh-my-pilot doctor` reports OMP active and GitHub auth valid via `gh`.
- Focused health/config tests pass.
- `make build` produces the current macOS arm64 binary.

## Next work

1. Observe a few real GitHub-labelled tasks end-to-end and inspect OMP provider
   errors directly if execution returns no changes.
2. Enable and test Jira polling only when Jira becomes the desired issue source;
   retain GitHub configuration for PR and CI supervision.
3. Address `doctor` warnings only if needed: sleep prevention, stale updater
   release metadata, and oversized repository documentation are non-blocking.
