---
created_on: 2026-09-24 17:53
status: current
---

# cron-person-cli

Higher-order per-directory crontab manager with direnv-style shell integration

## Commands
- **Build Local Binary:** `just build` (compiles to `bin/cron-person`)
- **Run CLI with Arguments:** `just run [args...]`
- **Run CLI in Agent Mode:** `just run-ai [args...]` (`AGENT=1`)
- **Run Tests:** `just test` (`go test -v ./...`)
- **Run Static Analysis & Tests:** `just check` (`go vet ./... && go test -v ./...`)
- **Run Linter / Static Analysis:** `just vet` or `just lint` (`go vet ./...`)
- **Format Source Code:** `just fmt` (`go fmt ./...`)

## Setup & Environment
- **Prerequisites:** Go 1.26.2+, `just`.
- **Temporary Files:** Temporary file operations use `.tmp/` within the project root.

## Conventions
- **Output Formatting & No Decorative Headers:** All CLI output must be plain text without emojis across all modes.
- **Help Screens & Terminal Width:** CLI help output (`--help`) must display an aligned hierarchical tree view with `├─` and `╰─` glyphs powered by `github.com/alexgorbatchev/cobra-help-tree/v2` (`cobrahelptree.Setup(rootCmd)`), with command descriptions automatically trimmed to the terminal width using ellipsis (`...`).
- **Agent Mode (`AGENT=1`):** When `AGENT=1` is set, output compact key-value pairs or bullets.
- **Hermetic Unit Tests:** All unit tests must remain 100% offline and hermetic.

## Boundaries
- **Always:** Maintain >= 90% statement code coverage across Go packages.
- **Always:** Run `just test` and `just vet` before committing changes.
- **Never:** Commit compiled Go binaries (e.g. `bin/cron-person`) to git.
