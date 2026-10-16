set dotenv-load := false

# Default recipe: list available recipes
default:
    @just --list

# Run CLI in human mode (default)
run *args:
    go run ./cmd/cron-person {{ args }}

# Run CLI in agent-facing mode
run-ai *args:
    AGENT=1 go run ./cmd/cron-person {{ args }}

# Run test suite
test:
    go test -v ./...

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

# Run static analysis and test suite in sequence
check:
    go vet ./...
    go test -v ./...

# Clean up build binaries and temporary files
clean:
    rm -rf bin coverage.out .tmp dist
