# oh-my-pilot Agent Guide

## Project intent

`oh-my-pilot` is a fork of Pilot that uses **Oh My Pi (`omp`) as its only
execution runtime**. Do not reintroduce Claude Code CLI configuration,
dependency checks, subprocesses, or documentation.

## Working conventions

- Keep changes focused and preserve the OMP-only architecture.
- Use `rg` for text/file discovery. Search the codebase-memory MCP graph first
  when it is available; fall back to `rg` when it is not.
- Read the nearest applicable `AGENTS.md` before editing files.
- Use `apply_patch` for file edits.
- Do not commit or push unless the user asks.
- Never print or commit credentials, tokens, local config, or OMP profile data.

## Build and verification

- Build the local macOS development binary with `make build`.
- The repository post-commit hook rebuilds only; it intentionally skips tests.
- Install the hook with `make install-hooks`.
- Run focused tests for changed packages before broader checks. The full suite
  can be slow, so do not run it unless requested or required for release work.
- The development binary is `bin/oh-my-pilot`.

## Runtime configuration

- Default config path: `~/.oh-my-pilot/config.yaml`.
- The shared OMP config root is `~/.omp`; RPC/session data uses `~/.omp/agent`.
- Configure the OMP executable explicitly when necessary, currently
  `/Users/fiscozen/.bun/bin/omp` on the maintainer's macOS machine.
- GitHub credentials resolve in this order: GitHub App, config token,
  `GITHUB_TOKEN`, then `gh auth token`. Prefer the authenticated `gh` CLI for
  local development instead of storing a token in config.
- The primary local workflow polls GitHub issues labelled `oh-my-pilot`, runs
  OMP in an isolated worktree, opens a PR, and monitors CI. Autopilot must keep
  `auto_merge: false` unless a user explicitly changes that policy.

## Current high-value areas

- OMP execution and provider-error handling: `internal/executor/`.
- GitHub/Jira polling and startup wiring: `cmd/pilot/`.
- Autopilot PR and CI supervision: `internal/autopilot/`.
- Operator dashboard and task reset controls: `internal/dashboard/` and
  `cmd/pilot/`.
- Configuration and health reporting: `internal/config/` and `internal/health/`.
