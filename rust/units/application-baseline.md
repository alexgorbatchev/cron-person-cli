---
created_on: 2026-11-25 08:08
last_modified: 2026-11-25 09:13
status: current
---

# Application characterization boundary

This ledger identifies what the migration's recordings prove for implementers. The Go application remains the default build. The temporary Rust entry point runs shell generation natively; unported commands still delegate to Go.

## Reference and reproducibility

The reference executable is `bin/cron-person-go-reference`, built from application revision `5fa7005813c99bc54ebef41447d4adb2aaa6c32a` with Go 1.26.11 on Linux amd64. Its SHA-256 is `c3e0ea13bd55454934b18b399da8311f274a65c3fe3435321415c2879064e3a5`; its version is `0.0.1`. The subsequent planning revisions do not change application source. Build an equivalent reference with `go build -o bin/cron-person-go-reference ./cmd/cron-person`; embedded VCS metadata can change its binary hash without changing behavior.

`reference/cli-inputs.mjs` derives the supplied cases from the preliminary inventory plus mutation, execution, history and invalid-byte cases. `reference/http-inputs.mjs` creates an ordered HTTP CRUD sequence. Both generators write JSONL beside themselves. Every command runs in isolated HOME/XDG directories with a fixture crontab executable; no user's crontab is called. The HTTP adapter replaces only the fixture paths and startup token/origin transport values.

From the repository root:

```sh
bun rust/reference/cli-inputs.mjs
rust-anything app record . --inputs rust/reference/cli-inputs.jsonl --normalize timestamps --timeout 8 --name cron-person --json -- bun rust/reference/cli-run.mjs bin/cron-person-go-reference
rust-anything app replay . --json
cargo build --manifest-path rust/app/Cargo.toml --locked
rust-anything app replay . --build 'bun rust/reference/cli-rust-run.mjs rust/app/target/debug/cron-person' --json
rust-anything app replay rust/http-baseline --json
rust-anything app replay rust/http-baseline --build 'bun ../reference/http-run.mjs ../../rust/app/target/debug/cron-person {port}' --json
```

The fixture adapter wraps the entire Rust entry point, so native and fallback cases observe the same isolated files and byte streams. The Rust fallback directly runs the Go reference. The Rust adapter additionally normalizes the exact Go build path when unported commands name their executable; it does not broaden task/run identity normalization. The HTTP harness independently supplies its isolation.

## Measured evidence

| Boundary | Result | Elapsed wall time |
| --- | --- | --- |
| CLI recording, two original runs per case | 556 cases, zero noisy/unstable/timed-out cases | 3.810 s |
| CLI original replay | 556/556 | 1.950 s |
| CLI Rust entry replay | 556/556, 556 Go fallbacks | 2.036 s |
| HTTP original replay | 337/337 | 0.360 s |
| HTTP Rust entry replay | 337/337, Go serves every route | 0.310 s |
| Existing Go suite, including display reference tests | `just test` passes | 0.442 s (mostly cached) |
| Go static analysis | `just vet` passes | 0.292 s |

The table above is the original all-fallback checkpoint. After native hook generation and fixture-boundary integration, original replay passes 556/556 (3.04 s), the per-unit kill switch passes 556/556 with all 556 fallbacks (2.19 s), and final native replay passes 556/556 with 553 fallbacks (2.35 s). HTTP remains 337/337 (0.32 s), entirely Go-served. Final application tests pass three process tests (0.32 s command, 0.06 s test execution), clippy passes (0.14 s), and required `just test` / `just vet` pass (7.55 s / 0.56 s, with cached packages). These are migration checkpoints, not standalone-runtime acceptance.

Native application process tests verify exact bash/zsh/fish generation, supported `--` separation, fallback for unported help/error/argument forms, the kill switch, eight agent-mode variants for real `/dev/full` write failures, and Unix SIGPIPE termination. Application coverage is 186/202 lines (92.08%) and 14/14 functions (100%), excluding tests and including unexecuted application source. Native hook export remains unported.

CLI coverage includes human and agent help at 40/80/unset widths; version and skill; hooks and parser failures; authorization changes and missing, modified, orphaned and corrupt records; config errors; crontab generation/sync/uninstall; task environment, exit failures and invalid stdout/stderr bytes; history/log limits and errors. File observations include contents, modes, links and derived task/run identity assertions.

HTTP coverage includes embedded index/style/script/notices, HEAD/missing files, token/origin enforcement, wrong methods/routes, document reads, invalid JSON/body limits, revision conflicts, invalid schedules, symlink refusals, allow/deny, create/update/delete and observed filesystem changes. Numbered scenario names and unique ignored request headers preserve the recorder's sorting/deduplication semantics.

The original interface has 34 inspected captures retained in `.state/rust-ui-baseline/`, with capture settings and provenance in `metadata.json` and paths in `screenshot-paths.txt`. Desktop is 1440×1000 and narrow is 390×844 at scale 1. The delegate inspected all images; the parent additionally inspected desktop editing and 80-column help. No crontab calls occurred; owned browser/server/fixtures were stopped/removed. Final Rust captures and independent blind review remain required.

## Explicit limits

- Linux recordings are application characterization, not native Windows/macOS execution evidence, race/fault injection, daemon scheduling or terminal detach evidence. The per-unit ledgers account for those separately.
- Named timestamp normalization and validated invocation-dependent identities are fixture-only. Stable output remains exact; binary streams are retained as bytes. The tool separately redacts its own working directory in valid UTF-8 output.
- HTTP recording ignores its built-in volatile/transport headers, including Date, Server, Content-Length and Last-Modified. The recorded security headers remain exact. Native HTTP tests must cover any omitted contractual header.
- The HTTP fallback counter measures log growth during requests, while this CLI entry point falls back once at server startup. Its reported zero is not proof of zero Go use. Actual route ownership remains entirely Go until implemented and checked independently.
- The corpus is checked in; detailed reports, original binary and screenshots are retained ignored evidence. No UI acceptance, standalone runtime or cutover is claimed here.
