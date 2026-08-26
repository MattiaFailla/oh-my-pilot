# oh-my-pilot — autonomous OMP development pipeline
# Multi-stage build for standalone oh-my-pilot binary (cmd/pilot)
#
# Runtime uses Ubuntu because oh-my-pilot executes the OMP RPC binary, git, and gh.

# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build static binary using modernc.org/sqlite (pure-Go, no CGO needed)
ARG VERSION=dev
ARG BUILD_TIME
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o /pilot \
    ./cmd/pilot

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM ubuntu:22.04

ARG OMP_VERSION=18.0.5

# Avoid interactive prompts during apt installs
ENV DEBIAN_FRONTEND=noninteractive

# Install runtime dependencies:
# - git, gh: required for git operations and GitHub API calls
# - curl, ca-certificates: HTTPS requests, certificate validation
# - OMP: pinned Oh My Pi RPC runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    ca-certificates \
    gnupg \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | gpg --dearmor -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends \
    gh \
	&& case "$(dpkg --print-architecture)" in \
		amd64) omp_arch=x64; omp_sha=d5a322af241cebe2662b3b792ff29d3ea6e61364328e916c9429065f346391ed ;; \
		arm64) omp_arch=arm64; omp_sha=9fa632da09cc6f2b625bb79aa291d52985f936a10408d278d550983f91460785 ;; \
		*) echo "unsupported OMP architecture: $(dpkg --print-architecture)" >&2; exit 1 ;; \
	esac \
	&& curl -fsSL "https://github.com/can1357/oh-my-pi/releases/download/v${OMP_VERSION}/omp-linux-${omp_arch}" -o /usr/local/bin/omp \
	&& echo "${omp_sha}  /usr/local/bin/omp" | sha256sum -c - \
	&& chmod 755 /usr/local/bin/omp \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user (UID 1000)
RUN useradd -m -u 1000 -s /bin/bash pilot

# Create directories for SQLite data and workspace execution
RUN mkdir -p /home/pilot/.oh-my-pilot/data /home/pilot/.omp /workspace \
    && chown -R pilot:pilot /home/pilot /workspace

# Copy binary from builder
COPY --from=builder /pilot /usr/local/bin/oh-my-pilot
RUN chmod 755 /usr/local/bin/oh-my-pilot

# Switch to non-root user
USER pilot

ENV PI_CODING_AGENT_DIR=/home/pilot/.omp

WORKDIR /workspace

# Gateway port
EXPOSE 9090

# Health check using /health endpoint (requires gateway to be running)
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -sf http://localhost:9090/health || exit 1

ENTRYPOINT ["/usr/local/bin/oh-my-pilot"]
CMD ["start", "--github", "--autopilot=stage"]
