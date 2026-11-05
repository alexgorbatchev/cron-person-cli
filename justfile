set dotenv-load := false

# Default recipe: list available recipes
default:
    @just --list

# Run CLI in human mode (default)
run *args: build
    ./bin/cron-person {{ args }}

# Run CLI in agent-facing mode
run-ai *args: build
    AGENT=1 ./bin/cron-person {{ args }}

# Run test suite
test:
    go test -v ./...

# Run generated crontab entries under a real cron daemon in a disposable container (needs Docker; takes about 5 minutes)
test-daemon:
    #!/usr/bin/env bash
    set -euo pipefail
    # testcontainers ignores Docker contexts, so export the current one.
    if [ -z "${DOCKER_HOST:-}" ]; then
        DOCKER_HOST="$(docker context inspect --format '{{{{.Endpoints.docker.Host}}')"
        export DOCKER_HOST
    fi
    go test -tags daemon -count=1 -v -timeout 20m -run TestDaemonRunsGeneratedEntries ./cmd/cron-person

# Check a published release's workflow, assets, checksums, and binary versions without changing anything
verify-release tag:
    GITHUB_TOKEN="${GITHUB_TOKEN:-$(gh auth token)}" go run ./scripts/verify-release {{ tag }}

# Measure status, task list, crontab show, the prompt hook, and allow at 10, 100, and 1000 tracked directories
bench:
    go test -run '^$' -bench . -benchmem -benchtime 5x ./cmd/cron-person

# Run the tests with the race detector and fail when any package is below 90% statement coverage, as CI does
coverage:
    bash scripts/check-coverage.sh

# Check code, dependencies, and the standard library for every released OS against the Go vulnerability database (needs network)
vuln:
    vc="$(go tool -n govulncheck)" && for goos in linux darwin windows; do echo "GOOS=$goos"; GOOS=$goos "$vc" -show version ./... || exit 1; done

# Build binary into bin/
build:
    mkdir -p bin
    go build -o bin/cron-person ./cmd/cron-person

# Build and replace the cron-person payload used by the dotfiles shim
dev-bootstrap $dotfiles_dir=(env_var("HOME") / ".dotfiles"):
    mkdir -p bin
    revision="$(git rev-parse --short HEAD)" && go build -ldflags "-X main.version=999.0.0-dev.$revision" -o bin/cron-person ./cmd/cron-person
    go run ./scripts/dev-bootstrap.go

# Install binary to GOPATH bin directory
install:
    go install ./cmd/cron-person

# Run static analysis and vet
lint:
    go vet ./...

# Alias for lint
vet: lint

# Format Go code
fmt:
    go fmt ./...

# Run static analysis, then the race-enabled tests and coverage gate CI runs
check: lint coverage

# Clean build outputs while preserving session-owned scratch
clean:
    rm -rf bin coverage.out dist

# Download project dependencies for local development
setup:
    go mod download
