# Port map: cron-person-cli-go

4 of 16 units have a Rust port; **2 changed upstream** since they were recorded: re-record them and run `diff`.

## Ports

| Unit | Crate | In Rust | Kept in the original | Cases | Last diff | Original |
| --- | --- | --- | --- | ---: | ---: | --- |
| `rust/reference/cronrc/` | `rust/cronrc-rs` | `Describe`, `FileCase`, `Hash`, `ParseBytes` | - | 656 | 414/656 | **changed**: `rust-anything record rust/cronrc-rs` |
| `rust/reference/cronrc/` | `rust/reference/cronrc-scaffold` | `Describe`, `FileCase`, `Hash`, `ParseBytes` | - | 0 | not run | unchanged |
| `rust/reference/display/` | `rust/display-rs` | `FormatPath`, `FormatErrorText`, `PathBytes`, `ErrorBytes` | - | 1975 | 1975/1975 | unchanged |
| `rust/reference/filelock/` | `rust/filelock-rs` | `Scenario` | - | 55 | 55/55 | **changed**: `rust-anything record rust/filelock-rs` |

## Not ported yet

In the order `analyze` suggests.

| Unit | Status | Lines | Exports | Readiness |
| --- | --- | ---: | ---: | ---: |
| `internal/display/` | ready | 51 | 2 | 92 |
| `internal/crontab/` | ready | 237 | 8 | 68 |
| `internal/hook/` | ready | 83 | 2 | 90 |
| `internal/config/` | ready | 69 | 3 | 82 |
| `scripts/` | ready | 73 | 0 | 88 |
| `internal/store/` | caution | 434 | 2 | 49 |
| `internal/filelock/` | caution | 57 | 7 | 72 |
| `internal/runner/` | caution | 272 | 1 | 54 |
| `scripts/verify-release/` | ready | 428 | 0 | 53 |
| `internal/cronrc/` | caution | 339 | 6 | 35 |
| `internal/history/` | caution | 645 | 3 | 19 |
| `cmd/cron-person/` | caution | 1623 | 0 | 0 |

Regenerate with `rust-anything map --write`. Commit this file: when the original changes upstream, the next map marks every port whose recording it outdated.
