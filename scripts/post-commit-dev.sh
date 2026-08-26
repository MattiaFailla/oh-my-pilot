#!/usr/bin/env bash
# Refresh the local development binary after each commit. The hook remains
# useful on machines without a host Go toolchain by using the same pinned Go
# container as local development builds.

set -u

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go_image="golang:1.25.0-bookworm"

run_native() {
	(
		cd "$project_root"
		make build
	)
}

run_docker() {
	local architecture
	case "$(uname -m)" in
		arm64) architecture="arm64" ;;
		x86_64) architecture="amd64" ;;
		*)
			echo "Unsupported macOS architecture: $(uname -m)" >&2
			return 1
			;;
	esac

	docker run --rm \
		-e GOOS=darwin \
		-e GOARCH="$architecture" \
		-v omp-go-cache:/go \
		-v omp-go-build-cache:/root/.cache/go-build \
		-v "$project_root":/app \
		-w /app \
		"$go_image" \
		sh -lc 'export PATH=/usr/local/go/bin:$PATH; make build'
}

echo ""
echo "Rebuilding the development binary after commit..."

if command -v go >/dev/null 2>&1; then
	run_native
elif command -v docker >/dev/null 2>&1; then
	run_docker
else
	echo "Cannot run post-commit checks: install Go or Docker." >&2
	exit 1
fi

if [ $? -ne 0 ]; then
	echo "Post-commit rebuild failed; the commit was created, but the development binary is stale." >&2
	exit 1
fi

echo "Post-commit rebuild passed; bin/oh-my-pilot is up to date."
