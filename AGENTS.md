---
created_on: 2026-09-24 17:53
last_modified: 2026-11-05 06:53
status: current
---

# cron-person-cli

Higher-order per-directory crontab manager with direnv-style shell integration

## Commands
- **Build Local Binary:** `just build` (compiles to `bin/cron-person`)
- **Install Development Build:** `just dev-bootstrap` builds version `999.0.0-dev.<revision>` and atomically replaces the payload referenced by the existing dotfiles shim. Override the dotfiles root with `just dev-bootstrap /path/to/dotfiles`; keep the shim and its symlinks intact.
- **Run CLI with Arguments:** `just run [args...]` (builds `bin/cron-person` and runs `./bin/cron-person`)
- **Run CLI in Agent Mode:** `just run-ai [args...]` (`AGENT=1 ./bin/cron-person`)
- **Run Tests:** `just test` (`go test -v ./...`)
- **Run Cron Daemon Integration Test:** `just test-daemon` installs generated entries into a real Debian cron inside a disposable testcontainers container and waits for real fires (about 5 minutes, needs Docker; `DOCKER_HOST` defaults to the current Docker context). It sits behind the `daemon` build tag, so `just test` stays hermetic.
- **Verify a Published Release:** `just verify-release <tag>` reads the tag's release workflow run, assets, `checksums.txt`, and each binary's embedded version from GitHub without changing anything; run it after every release.
- **Measure Capacity:** `just bench` benchmarks `status`, `task list`, `crontab show`, the prompt hook, and `allow` with 10, 100, and 1000 tracked directories of ten-task `.cronrc` files (`cmd/cron-person/capacity_test.go`). Run it before and after changes to whole-store paths.
- **Check Coverage and Races:** `just coverage` runs the suite under the race detector and fails when any package's own tests cover less than 90% of its statements (`.testcoverage.yml`, `go tool go-test-coverage`). CI and the release `verify` job run the same commands. The race detector only sees one test process; cross-process behavior needs the subprocess fixtures.
- **Scan for Vulnerabilities:** `just vuln` runs `go tool govulncheck -show version ./...` once per released `GOOS` (linux, darwin, windows), checking the code, dependencies, and the standard library of the `toolchain` line in `go.mod` against the Go vulnerability database and logging the scanner and database versions. It needs network, so it is not part of `just check`; CI and the release `verify` job run it on every change and before publishing.
- **Run Static Analysis & Tests:** `just check` (`just vet`, then `just coverage`: the same race-enabled tests and coverage gate CI runs)
- **Run Linter / Static Analysis:** `just vet` or `just lint` (`go vet ./...`)
- **Format Source Code:** `just fmt` (`go fmt ./...`)

## Setup & Environment
- **Prerequisites:** Go 1.26.2+ (`go.mod` pins the patched `toolchain` that builds and releases use; `GOTOOLCHAIN=auto` fetches it), `just`.
- **Temporary Files:** Temporary file operations use an invocation-owned child of `.tmp/` within the project root. `scripts/check-coverage.sh` passes its unique profile to the native coverage tool, stops consumer process groups, and removes owned scratch on success, failure, and cancellation. Preserve scratch if shutdown cannot be verified; never remove another invocation's directory. Local checks, CI, and release verification use this recipe.

## Conventions
- **Output Formatting & No Decorative Headers:** All CLI output must be plain text without emojis across all modes.
- **Help Screens & Terminal Width:** CLI help output (`--help`) must display an aligned hierarchical tree view with `├─` and `╰─` glyphs powered by `github.com/alexgorbatchev/cobra-help-tree/v4` (`cobrahelptree.SetupWithOptions` in `setupHelp`), with long help text wrapped to the terminal width and never truncated. Human help takes its width from `$COLUMNS`, then the terminal; output that is neither sized nor a terminal stays unwrapped, and a single word wider than the screen stays whole on its own line. Agent help (`AGENT=1`) is never wrapped or shortened. `cmd/cron-person/help_width_test.go` pins this contract.
- **Agent Mode (`AGENT=1`):** When `AGENT=1` is set, output compact key-value pairs or bullets.
- **Hermetic Unit Tests:** All unit tests must remain 100% offline and hermetic. Check every fixture setup error (files, directories, approvals) instead of discarding it. Discovery tests place a `.cronrc` at the fixture root so the ancestor walk never leaves the fixture; only the no-`.cronrc` case depends on real ancestors, and it skips, naming the file, when one exists.
- **Agent Skill:** `skills/cron-person/SKILL.md` is the operating reference for agents; `cron-person skill` prints only the install command and URL that `cobra-help-tree` generates from `skillSource` in `cmd/cron-person/help.go`, never the skill itself. `TestSkillDocumentsEveryCommandAndFlag` fails when the skill omits a command, flag, argument, or catalog environment variable.
- **Run History:** Record every `exec` and `task run` invocation through `runner.ExecOptions.History`, including refused runs. Task execution must never depend on history, configuration, or pruning succeeding: report those failures after the task finishes. Determine run liveness with the run's OS-held lock (`internal/filelock`), never by probing PIDs. A foreground `task run` delegates recording to a `cron-person exec` child in its own session (`attach.go`), so capture survives detaching; tests re-execute the test binary as the CLI through `TestMain` (`CRON_PERSON_TEST_AS_CLI`).
- **Generated Commands:** Keep the entire task command inside one shell argument, escape cron percent signs, and pass the absolute authorization store and configuration paths explicitly. Test generated entries through the outer shell as in `cmd/cron-person/generated_test.go`.

## Boundaries
- **Always:** Maintain >= 90% statement code coverage in every Go package; `just coverage` and CI enforce it.
- **Always:** Run `just test` and `just vet` before committing changes.
- **Always:** Update `skills/cron-person/SKILL.md` and its `last_modified` in the same change as any command, argument, flag, default, environment variable, configuration setting, output, or side-effect change.
- **Always:** Resolve a reachable vulnerability that `just vuln` reports by upgrading the affected module, or the `toolchain` line to the patched Go release, before releasing. Rerun a scan that fails for network or database reasons; never skip or silence it.
- **Never:** Commit compiled Go binaries (e.g. `bin/cron-person`) to git.
