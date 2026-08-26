# oh-my-pilot

An autonomous ticket-to-PR pipeline powered exclusively by [Oh My Pi (OMP)](https://github.com/can1357/oh-my-pi).

oh-my-pilot receives tickets, prepares isolated worktrees, runs OMP through its JSONL RPC protocol, enforces quality gates, and manages GitHub pull-request lifecycle operations.

## What Changed

This fork is a clean break from Pilot's previous execution stack:

- OMP `18.0.5` is the only agent runtime.
- Claude Code, Qwen Code, OpenCode, and direct Anthropic/OpenAI executor backends are unsupported.
- Provider credentials and model selection belong to the OMP profile, not oh-my-pilot configuration.
- User state lives in `~/.oh-my-pilot`; project instructions use `AGENTS.md`.
- The executable is `oh-my-pilot`.

## Requirements

- Go `1.25+` to build from source
- OMP `18.0.5` or the pinned container image
- Git
- `gh` and `GITHUB_TOKEN` for GitHub polling and pull requests

## Quick Start

```bash
git clone https://github.com/qf-studio/oh-my-pilot
cd oh-my-pilot
make build

# Ensure OMP is configured with your provider profile.
omp --version

mkdir -p ~/.oh-my-pilot
cp configs/oh-my-pilot.example.yaml ~/.oh-my-pilot/config.yaml

./bin/oh-my-pilot doctor
./bin/oh-my-pilot start --github
```

## Build on macOS

With Go `1.25+` installed locally, build a native binary with:

```bash
make build
./bin/oh-my-pilot version
```

If Go is not installed locally, use Docker to produce a native macOS binary:

```bash
# Apple Silicon
docker run --rm -e GOOS=darwin -e GOARCH=arm64 \
  -v "$PWD":/app -w /app golang:1.25.0-bookworm \
  sh -lc 'export PATH=/usr/local/go/bin:$PATH; make build'

# Intel Mac: replace GOARCH=arm64 with GOARCH=amd64.
```

The executable is written to `bin/oh-my-pilot`.

## Docker Compose

```bash
cp configs/oh-my-pilot.example.yaml config.yaml
mkdir -p omp-profile

# Populate omp-profile using your OMP profile-management workflow.
export GITHUB_TOKEN="ghp_..."
docker compose up -d
docker compose logs -f oh-my-pilot
```

The OMP profile mounts read-only at `/home/pilot/.omp` and is supplied to OMP through `PI_CODING_AGENT_DIR`.

## Configuration

```yaml
executor:
  type: omp
  omp:
    command: omp
    version: "18.0.5"
    profile_dir: /home/pilot/.omp
```

Do not add provider API keys or endpoint settings to this file. See the [OMP execution runtime documentation](docs/content/concepts/execution-backends.mdx) and [OMP profiles guide](docs/content/guides/omp-profiles.mdx).

The full [configuration example](configs/oh-my-pilot.example.yaml) includes a review-first autopilot setup: it polls either GitHub issues or Jira issues, runs OMP, opens pull requests, and monitors CI with `auto_merge: false`. Jira mode still requires `adapters.github.token` and `adapters.github.repo`, because GitHub remains the pull-request and CI destination.

## Development

```bash
make test
make lint
```

The source module intentionally remains `github.com/qf-studio/pilot` until the fork's GitHub repository path is finalized; this avoids publishing a guessed module identity.
