---
created_on: 2026-11-25 09:26
last_modified: 2026-11-25 09:48
status: current
---

# Independent crontab operations

This ledger gives maintainers the implementation boundary, differential evidence and remaining work for the independent crontab operations. The Linux implementation of removal, merging and system-command reads/writes passes its unit gate. **The crontab package and application are incomplete.** The independent library is ready for source integration; application wiring remains unfinished.

## Production API and side-effect inventory

The complete production source inspected is `internal/crontab/crontab.go`, including every exported API and its helpers. The reference package imports this production package; it does not reproduce its algorithms.

| Production boundary | Native boundary | Behavior and status |
| --- | --- | --- |
| `BLOCK_START`, `BLOCK_END` | `native::BLOCK_START`, `native::BLOCK_END` | Exact marker bytes. |
| `RemoveBlock` (line 146) | `native::remove_block(&[u8])` | Implemented. Exact whole marker lines; one CR before LF/EOF is accepted. Removes through the end marker's LF, preserving every other byte. Nested, repeated, unexpected-end and unclosed errors retain encounter precedence and exact diagnostics. |
| `MergeCrontab` (line 124) | `native::merge_crontab(&[u8], &[u8])` | Implemented. Removal errors precede merging. Trims only trailing LF bytes; adds the original separator/final LF rules. Accepts arbitrary replacement bytes without validating them. |
| `ReadSystemCrontab` (line 188) | `native::SystemCrontab::read()` | Implemented on Unix; runtime verified on Linux. Executes `crontab -l` directly, null stdin, captures stdout/stderr. Successful stderr is ignored. Any failed read whose stderr contains exact `no crontab for` bytes returns empty success. Other failures retain the context, process diagnostic, colon/space suffix and raw stderr. |
| `WriteSystemCrontab` (line 207) | `native::SystemCrontab::write(&[u8])` | Implemented on Unix; runtime verified on Linux. Executes `crontab -`, sends exact input bytes, discards stdout and captures stderr. Process failure precedes stdin-copy failure. Successful early stdin closure ignores broken pipe, as Go does. |
| `Invocation` (line 23) | Absent | Incomplete; belongs to generation integration. |
| `GenerateBlock` (line 31), `absRequired` (line 80), `quoteCronArg` (line 95) | Absent | Incomplete. Requires native store directory snapshots and invocation configuration. Original behavior validates absolute store/config paths before task processing, selects the executable fallback, rejects control bytes in paths/tasks, skips directories without tasks, shell-quotes arguments, escapes cron percent/backslash rules and omits cronrc environment assignments. No replacement store or scheduler is provided. |
| `Preview` (line 221) | Absent | Incomplete. Calls the real store's `Schedulable`, generates from its snapshots, returns ready/skipped counts and wraps generation failure. |
| `Sync` (line 236) | Absent | Incomplete. Takes the state-directory update lock before preview/read/merge/write; failures return zero/no skipped results. Releases the lock best effort. |
| `Uninstall` (line 265) | Absent | Incomplete. Takes the same lock, reads/removes/writes without merge newline normalization; works independently of damaged store contents. |
| `lockUpdates` (line 289) | Absent | Incomplete with update integration. Creates the state directory with mode 0755, locks `crontab.lock`, and wraps lock failures. The independent Go read/write functions take no lock, so their Rust counterparts do not introduce one. External edits during an update remain outside that lock's protection. |

`SystemCrontab` receives an explicit per-call environment and working directory. `SystemCrontab::inherited()` captures the caller's environment/current directory without executing anything. PATH lookup uses effective-ID executable access, rejects relative executables with Go's ErrDot diagnostic unless the last `GODEBUG=execerrdot` setting disables that check, and preserves relative paths in spawn errors when allowed. `Error.bytes` is authoritative for arbitrary stderr/path bytes; `Display` supplies a lossy UTF-8 view.

The dedicated stdin writer blocks SIGPIPE only in its own short-lived thread. This preserves Go's broken-pipe behavior when the library is loaded by a cgo host that would otherwise terminate on SIGPIPE. No global environment or signal disposition is changed. Output draining and stdin writing run concurrently to avoid pipe deadlock. No real user crontab is invoked by any new test or reference adapter: PATH contains only an invocation-owned fake executable, and each subprocess has its own environment/directory.

## Source layout and consumption

| Path | Purpose |
| --- | --- |
| `rust/crontab-rs/src/native.rs` | Actual byte-oriented library and Unix process implementation; forbids unsafe code. |
| `rust/crontab-rs/src/entry.rs` | Public native module; optional bridge entry. |
| `rust/crontab-rs/src/port.rs` | Test-only scalar/hex adapters and isolated fake-command fixtures; not application implementation. |
| `rust/crontab-rs/src/lib.rs`, `src/wire.rs` | Generated optional FFI bindings; unchanged generated unsafe blocks carry safety comments. |
| `rust/crontab-rs/Cargo.toml`, `Cargo.lock` | Standalone crate and pinned dependencies. Unix dependencies use native `filelock-rs` with `default-features = false` for OS diagnostic bytes, rustix for executable access, and nix for the thread-local signal mask. No filelock source is changed. |
| `rust/reference/crontab/crontab.go` | Genuine adapters: `Text` calls production removal/merge; the child protocol calls production read/write. Only random fixture-directory prefixes in diagnostics are normalized. Bytes cross JSON as hex. |
| `rust/reference/crontab/crontab_test.go` | Recorded-corpus replay, actual production-call child protocol tests, isolated test executables and real reader/output failures. |
| `rust/reference/crontab/probe/main.go`, `main_test.go` | Covered Go executable and protocol tests for returned bytes, errors, malformed calls and failed output. |
| `rust/reference/crontab-inputs.py`, `crontab-inputs.jsonl` | Deterministic input generation and 1,651 explicit cases. |
| `rust/reference/crontab-coverage.py` | Genuine covered probe replay, real child tests and merged statement-threshold assertions. |
| `rust/crontab-rs/tests/contracts.rs`, `tests/process.rs` | Native text corpus replay, complete bridge replay and direct native subprocess tests. |
| `rust/crontab-rs/go/`, `go/shadow/` | Generated cgo and shadow wrappers; recorded-case shadow tests. |
| `rust/crontab-rs/cases.jsonl`, `recorded-from.json`, `rust-anything.json` | Original observations, source fingerprint and bridge manifest. |

Consume with `crontab-rs = { path = "../crontab-rs", default-features = false }`. This exposes the native API without compiling FFI exports or fixture adapters. A release build with that feature setting exports no `rust_anything_` symbols, verified with `nm -D --defined-only`. The shared parent-created repository `.venv` is preserved. Running `uv run --no-project python` from the repository root selects its `.venv/bin/python` and `.venv` prefix, verified by assertions.

## Characterization and original test inventory

Two original recordings precede native implementation; two final recordings after narrowing the manifest to the actual adapter file are byte-identical to those originals. There are **1,793 cases: 1,674 Text and 119 Process**, including 1,275 returned errors. Generated scalar cases supplement the explicit corpus. There are no known-diff allowances, edited goldens, tolerance changes or fake production implementations.

Text cases enumerate all marker/job/empty-line permutations of lengths zero through four with LF and CRLF, malformed-marker precedence, exact marker substrings, empty inputs/blocks, trailing LF rules, CR at EOF, all 256 byte values, NUL/invalid UTF-8 and seeded random byte payloads. Process cases cover both argument forms, raw stdout/stderr/stdin, success and exit statuses 1/2/7/127/255, exact no-crontab suppression, missing/non-executable/invalid-format/missing-interpreter/relative executables, SIGTERM, closed stdin, 128 KiB pipes and 1 MiB input. Adapter validation errors are also recorded.

| Original tests inspected and executed | Coverage represented by this native unit |
| --- | --- |
| `TestMergeCrontab_NewBlock`, `TestMergeCrontab_ReplaceExistingBlock`, `TestRemoveBlock`, `TestRemoveAndMergeBlock_MalformedAndRepeatedBlocks`, `TestMergeCrontabEmptyInputs` | Removal/merge behavior represented and extended by native byte corpus. |
| `TestSystemCrontabRoundTrip`, `TestSystemCrontabFailures` | Independent process behavior represented and extended by fake-executable differential cases/native tests. |
| `TestQuoteCronArgRoundTrip`, `TestGenerateBlockRejectsInvalidPaths`, `TestGenerateBlock`, `TestGenerateBlockKeepsCronrcVariablesOutOfTheCrontab`, `TestGenerateBlock_ExecutesSafelyAndBlocksUnauthorized`, `TestGenerateBlock_RejectsInvalidControlCharacters`, `TestGenerateBlockSkipsDirectoriesWithoutTasks` | Go tests pass; generation, quoting, authorization enforcement and generated-shell execution are not ported or covered by native generation tests. |
| `TestHelperCronPersonRunner`, `cronPreprocessCommand` and fixture helpers | Inspected as original test infrastructure; no native substitute is installed. |
| `TestPreviewSkipsChangedDirectories`, `TestPreviewSkipsSpecialFilesWithoutBlocking` | Go tests pass; native store filtering, changed/missing/special-file handling are incomplete. |
| `TestSyncAndUninstall`, `TestSyncAndUninstallFailures`, `TestSyncAndUninstallPreserveMarkerSubstringsAndRejectMalformed` | Go tests pass; only their underlying independent text/process boundaries are characterized here. Native update flows are incomplete. |
| `TestConcurrentUpdatesWaitForTheUpdateInFlight`, `TestUpdatesOverwriteExternalEditsMadeInFlight`, `TestUpdatesReportUnavailableLock` | Go tests pass; no native update lock/store integration or external-edit policy is claimed. |

Original test sources are `internal/crontab/crontab_test.go`, `system_test.go` and `special_unix_test.go`, all read in full. The native process tests additionally exercise eight simultaneous isolated commands with non-UTF-8/metacharacter paths, exact argument counts, environment capture, repeated GODEBUG precedence, relative spawn diagnostics, error sources, simultaneous large stdin/stderr, ignored successful stdout and exit-before-copy-error precedence. Fixture writes are serialized around the parallel command group to avoid transient executable-write races; command execution within that group remains parallel.

## Coverage and gate evidence

| Measured source | Result |
| --- | --- |
| Native `src/native.rs` | **252/263 lines (95.82%), 30/32 functions (93.75%)**. JSON assertions enforce both >=90%. |
| Optional adapter `src/port.rs` | 101/103 lines (98.06%), 10/10 functions (100%). |
| Genuine Go adapter `rust/reference/crontab/crontab.go` | **90.2% statements** in the ordinary race-enabled package test profile. The earlier subprocess-counter aggregate measured 93/102 statements (91.18%). |
| Go probe `probe/main.go` | **96.6% statements** in the ordinary race-enabled package test profile. Both packages pass the existing per-package 90% checker; combined ordinary coverage is 120/131 statements (91.6%). |
| Original production package `internal/crontab` | Race-enabled original suite passes at **96.6% statement coverage**; all four ported production functions reach 100% statement coverage in that suite. |
| Differential and generated shadow wrappers | **1,793/1,793** exact observations; old, shadow and new modes pass, shadow mismatches zero. |
| `rust-anything gate` | Passes formatting, clippy, tests, unsafe audit, lockfile, implementation, errors, panic/API/source-freshness/build and exact behavior checks. |
| Size check | **489 Rust lines / 173 Go lines (2.8x), default 3x limit unchanged**. The manifest selects only `crontab.go`, excluding the Go tests/probe from the denominator. Native Rust is included; no padding, hidden Rust or threshold relaxation. |

The native threshold includes the complete native module. Generated FFI/wire code is outside that native metric (its direct Rust-test coverage is 13.07%/0% of lines); the generated cgo wrappers are exercised separately by differential/shadow execution. Unhit native paths include wait failures, non-broken-pipe stdin-copy failures, non-errno diagnostic fallback and the core-dump suffix; OS failure injection is not substituted for actual execution. Linux SIGTERM is runtime-tested; other signal names are checked against the installed Go Linux/Darwin signal tables, not claimed runtime-tested.

Parent integration found that the original adapter's ordinary package tests covered only 3.9%, despite the passing separate subprocess aggregate. Corpus replay and direct production-call tests now make the standard profile pass without exclusions or threshold changes. Probe tests cover malformed input and output failures; malformed calls return diagnostics instead of panicking. Synchronous fixture children disable only the race detector's exit sleep, retaining instrumentation. Their two new original recordings are byte-identical to all 1,793 existing observations. Output-failure fixtures use an actual failing writer instead of the Linux-only `/dev/full` device. Unhit adapter paths include package-init process exit, the Windows fixture guard and exceptional fixture/protocol failures. This scoped result does not establish coverage for the other in-progress reference packages.

## Reproducing coverage

Run from the repository root. `$E` below is an invocation-owned ignored directory; create it uniquely and remove it after retaining the printed summary. Compiler scratch can be scoped to a separate child using `GOTMPDIR` for the build only. Test fixtures retain system-temp defaults and clean themselves.

```sh
E=$(mktemp -d "$PWD/.tmp/crontab-coverage.XXXXXX")
go test -race -count=1 -coverprofile="$E/reference.cover" ./rust/reference/crontab/...
go tool go-test-coverage --config=.testcoverage.yml --profile="$E/reference.cover"
mkdir -p "$E/gocover" "$E/build"
GOTMPDIR="$E/build" go build -covermode=atomic -cover -coverpkg=./rust/reference/crontab/... -o "$E/probe" ./rust/reference/crontab/probe
uv run --no-project python rust/reference/crontab-coverage.py "$E/probe" "$E/gocover"
cargo llvm-cov --manifest-path rust/crontab-rs/Cargo.toml --locked --json --output-path "$E/native.json"
uv run --no-project python -c 'import json,sys; f=next(f for f in json.load(open(sys.argv[1]))["data"][0]["files"] if f["filename"].endswith("/native.rs")); print(f["summary"]); assert all(f["summary"][k]["percent"] >= 90 for k in ("lines","functions"))' "$E/native.json"
```

The Go reference source's package-init children exit before main's coverage hook. Covered test subprocesses execute the same `serveChild`/production calls after Go's test coverage setup and emit real counters; the aggregate preserves uncovered statements rather than manufacturing counters. No production adapter behavior is replaced for this measurement.

## Benchmark and platform limits

The final parent bridge-inclusive benchmark measures Text at 510.56 ns in Go versus 1,446.46 ns in Rust (**0.353x; Rust 2.83x slower**), and Process at 2,141,884.76 ns versus 1,478,379.22 ns (**1.449x**). These timings include adapters, fixture setup/cleanup and the Go reference's extra process re-execution. They are not native application latency measurements or a claim that rewriting text operations improves speed. This is an approved migration, with the bridge slowdown disclosed; no performance-based activation is performed.

Linux x86_64 is the only runtime-tested platform. `aarch64-apple-darwin` native process/text compilation passes; Darwin execution, signal behavior and permission/errno diagnostics still require native acceptance. `x86_64-pc-windows-gnu` compilation passes **for text only**: no Windows `SystemCrontab` implementation/export exists. Other Unix targets and the other release architectures are unverified. No Windows subprocess compatibility or complete cross-platform crontab port is claimed.

No UI output changes or screenshots apply to this library-only work. The parent runs root `just test` and `just vet` before publication. Whole-application replay, root `just coverage`, release vulnerability scans and daemon integration remain application acceptance work. The existing parser/prose and source-size decisions remain unresolved by this work.

## Handoff boundary

The independent delivery contains only `rust/crontab-rs/**`, `rust/reference/crontab*` and this ledger. The parent repaired portable manifest metadata and ordinary Go test coverage before source publication. The native filelock dependency and the shared `.venv` are preserved.

The parent can consume the four verified native operations with bridge features disabled, then integrate the real store before implementing generation, preview, sync, uninstall and their lock/error/count behavior. Windows command behavior and native macOS acceptance remain explicit remaining work. The generated gate proves only this independent unit; it does not complete the crontab package or application. Private ownership/checkpoint information is stored only in ignored private state and the tracker.

## Command record

Parent integration reruns the unchanged gate after the reference repairs: **all 12 checks and 1,793 comparisons pass in 1.97 seconds**, with the same 489/173 source-line counts. Plain native consumption passes four tests in 0.48 seconds. Two genuine original recordings take 0.75 and 0.57 seconds and retain the recording hash below. The final standard race-enabled coverage run takes 3.67 seconds; the final benchmark takes 1.83 seconds. Old, shadow and new wrapper modes each replay 1,793 exact cases with zero mismatches. Root `just test` and `just vet` pass in 1.99 and 0.73 seconds. These runs supplement the original worker command record below.

Cleanup completes with zero owned live processes or fixture directories. Two fixture directories left by the initial SIGPIPE-terminated drivers are verified absent from the pre-task baseline, checked for consumers and removed. Other workers' new temp entries are preserved. Completed llvm-cov target output, raw Go counters/profiles, full coverage exports, covered probe executable, duplicate recording and rust-anything driver scratch are removed. Normal debug/release/incremental and cross-check caches remain. Final owned artifacts total approximately **322 MiB**, below the 750 MB limit; observed build/coverage snapshots also remain below the limit. The shared repository `.venv` and other units' artifacts are retained.

`cargo llvm-cov clean --manifest-path rust/crontab-rs/Cargo.toml --locked` takes 0.124071 seconds; owned scratch/fixture cleanup takes 0.012044 seconds. A fresh coverage run regenerates the removed data using the commands above. The validation summaries remain available; no processes or fixtures require handoff cleanup.

Durations are wall-clock seconds measured around each actual command, including startup and any package-cache contention. Ignored evidence paths are represented by `$E` to omit private ownership details. Earlier failed attempts remain evidence of the recovery, not successful checks. Additional symbol, interpreter, hash, coverage-threshold and cleanup assertions are recorded alongside the retained summaries.

| Run | Command | Seconds | Exit |
| --- | --- | ---: | ---: |
| bridge | `rust-anything bridge . rust/reference/crontab/ --out rust/crontab-rs` | 0.240884 | 0 |
| record-first | `rust-anything record rust/crontab-rs --count 40 --inputs rust/reference/crontab-inputs.jsonl` | 1.131425 | 0 |
| record-second | `rust-anything record rust/crontab-rs --count 40 --inputs rust/reference/crontab-inputs.jsonl` | 0.577065 | 0 |
| native-first | `cargo test --manifest-path rust/crontab-rs/Cargo.toml` | 3.156114 | 0 |
| tests-first | `cargo test --manifest-path rust/crontab-rs/Cargo.toml --locked` | 1.104025 | 101 |
| tests-fixed | `cargo test --manifest-path rust/crontab-rs/Cargo.toml --locked` | 1.010970 | 101 |
| tests-fixed2 | `cargo test --manifest-path rust/crontab-rs/Cargo.toml --locked` | 0.777703 | 101 |
| tests-fixed3 | `cargo test --manifest-path rust/crontab-rs/Cargo.toml --locked` | 0.812100 | 0 |
| build | `rust-anything build rust/crontab-rs` | 0.580728 | 0 |
| diff | `rust-anything diff rust/crontab-rs` | 0.844601 | 2 |
| bench | `rust-anything bench rust/crontab-rs --json` | 1.461265 | 2 |
| coverage-initial | `cargo llvm-cov --manifest-path rust/crontab-rs/Cargo.toml --locked --no-default-features --json --output-path $E/native-coverage.json` | 2.828028 | 101 |
| coverage-retry | `cargo llvm-cov --manifest-path rust/crontab-rs/Cargo.toml --locked --no-default-features --json --output-path $E/native-coverage.json` | 0.745232 | 101 |
| coverage-clean-fixtures | `cargo llvm-cov --manifest-path rust/crontab-rs/Cargo.toml --locked --no-default-features --json --output-path $E/native-coverage.json` | 0.766258 | 0 |
| build-sigpipe | `rust-anything build rust/crontab-rs` | 2.287814 | 0 |
| shadow | `rust-anything shadow rust/crontab-rs` | 0.019175 | 0 |
| go-probe-build | `go build -cover -coverpkg=./rust/reference/crontab -o $E/probe ./rust/reference/crontab/probe` | 0.315146 | 0 |
| diff-sigpipe | `rust-anything diff rust/crontab-rs --json` | 0.723063 | 1 |
| bench-sigpipe | `rust-anything bench rust/crontab-rs --json` | 2.036003 | 0 |
| go-reference-covered | `uv run --no-project python rust/reference/crontab-coverage.py $E/probe $E/gocover` | 0.511607 | 1 |
| record-final1 | `rust-anything record rust/crontab-rs --count 40 --inputs rust/reference/crontab-inputs.jsonl` | 0.607407 | 0 |
| coverage-full | `cargo llvm-cov --manifest-path rust/crontab-rs/Cargo.toml --locked --json --output-path $E/native-coverage-full.json` | 3.977738 | 0 |
| go-reference-covered-fixed | `uv run --no-project python rust/reference/crontab-coverage.py $E/probe $E/gocover` | 0.482855 | 0 |
| record-final2 | `rust-anything record rust/crontab-rs --count 40 --inputs rust/reference/crontab-inputs.jsonl` | 0.588284 | 0 |
| shadow-mode | `env RUST_ANYTHING_MODE=shadow go -C rust/crontab-rs/go test -count=1 -v ./shadow` | 1.066883 | 0 |
| go-reference-percent | `go tool covdata percent -i=$E/gocover` | 0.430619 | 0 |
| clippy | `cargo clippy --manifest-path rust/crontab-rs/Cargo.toml --locked --all-targets -- -D warnings` | 1.581057 | 101 |
| go-original-tests | `go test -race -count=1 -coverprofile=$E/production.cover ./internal/crontab` | 4.060754 | 0 |
| gate-initial | `rust-anything gate rust/crontab-rs --json` | 4.451052 | 1 |
| go-probe-build-all | `go build -covermode=atomic -cover -coverpkg=./rust/reference/crontab/... -o $E/probe ./rust/reference/crontab/probe` | 0.308522 | 0 |
| go-reference-covered-all | `uv run --no-project python rust/reference/crontab-coverage.py $E/probe $E/gocover` | 0.478645 | 0 |
| no-default | `cargo test --manifest-path rust/crontab-rs/Cargo.toml --locked --no-default-features` | 1.911197 | 101 |
| go-probe-build-flush | `go build -covermode=atomic -cover -coverpkg=./rust/reference/crontab/... -o $E/probe ./rust/reference/crontab/probe` | 0.297563 | 0 |
| go-reference-flush | `uv run --no-project python rust/reference/crontab-coverage.py $E/probe $E/gocover` | 0.430036 | 1 |
| go-reference-tests | `env CRONTAB_COUNTERS=$E/gocover go test -count=1 -covermode=atomic -coverprofile=$E/reference-tests.cover ./rust/reference/crontab` | 0.575571 | 1 |
| no-default-fixed | `cargo test --manifest-path rust/crontab-rs/Cargo.toml --locked --no-default-features` | 0.616662 | 0 |
| go-probe-final | `go build -covermode=atomic -cover -coverpkg=./rust/reference/crontab/... -o $E/probe ./rust/reference/crontab/probe` | 0.639729 | 0 |
| go-reference-final | `uv run --no-project python rust/reference/crontab-coverage.py $E/probe $E/gocover` | 0.455472 | 0 |
| gate-fixed | `rust-anything gate rust/crontab-rs --json` | 3.066494 | 1 |
| go-reference-tests-fixed | `env CRONTAB_COUNTERS=$E/gocover go test -count=1 -covermode=atomic -coverprofile=$E/reference-tests.cover ./rust/reference/crontab` | 0.292260 | 1 |
| go-reference-tests-full-fixed | `env CRONTAB_COUNTERS=$E/gocover go test -count=1 -covermode=atomic -coverprofile=$E/reference-tests.cover ./rust/reference/crontab` | 0.314221 | 0 |
| record-source1 | `rust-anything record rust/crontab-rs --count 40 --inputs rust/reference/crontab-inputs.jsonl` | 0.767945 | 0 |
| record-source2 | `rust-anything record rust/crontab-rs --count 40 --inputs rust/reference/crontab-inputs.jsonl` | 0.578102 | 0 |
| go-vet | `go vet ./internal/crontab ./rust/reference/crontab/...` | 0.153002 | 0 |
| shadow-old | `env RUST_ANYTHING_MODE=old go -C rust/crontab-rs/go test -count=1 -v ./shadow` | 0.820116 | 0 |
| coverage-final | `cargo llvm-cov --manifest-path rust/crontab-rs/Cargo.toml --locked --json --output-path $E/native-coverage-final.json` | 1.700793 | 0 |
| gate-source | `rust-anything gate rust/crontab-rs --json` | 1.816896 | 0 |
| cross-windows | `cargo check --manifest-path rust/crontab-rs/Cargo.toml --locked --no-default-features --target x86_64-pc-windows-gnu` | 0.785628 | 0 |
| cross-darwin | `cargo check --manifest-path rust/crontab-rs/Cargo.toml --locked --no-default-features --target aarch64-apple-darwin` | 1.899248 | 0 |
| cross-darwin-clean | `cargo check --manifest-path rust/crontab-rs/Cargo.toml --locked --no-default-features --target aarch64-apple-darwin` | 0.116048 | 0 |
| cross-windows-clean | `cargo check --manifest-path rust/crontab-rs/Cargo.toml --locked --no-default-features --target x86_64-pc-windows-gnu` | 0.203485 | 0 |
| native-release | `cargo build --manifest-path rust/crontab-rs/Cargo.toml --locked --release --no-default-features` | 1.758012 | 0 |
| build-final | `rust-anything build rust/crontab-rs` | 0.586260 | 0 |
| diff-final | `rust-anything diff rust/crontab-rs --json` | 0.886192 | 0 |
| shadow-new | `env RUST_ANYTHING_MODE=new go -C rust/crontab-rs/go test -count=1 -v ./shadow` | 1.152539 | 0 |
| go-reference-aggregate | `uv run --no-project python rust/reference/crontab-coverage.py $E/probe $E/gocover` | 1.804233 | 0 |
| no-default-clippy | `cargo clippy --manifest-path rust/crontab-rs/Cargo.toml --locked --no-default-features --all-targets -- -D warnings` | 2.335644 | 0 |
| coverage-delivery | `cargo llvm-cov --manifest-path rust/crontab-rs/Cargo.toml --locked --json --output-path $E/native-coverage-delivery.json` | 1.841604 | 0 |
| bench-final | `rust-anything bench rust/crontab-rs --json` | 2.116261 | 0 |
| gate-final | `rust-anything gate rust/crontab-rs --json` | 2.294304 | 0 |
| coverage-clean | `cargo llvm-cov clean --manifest-path rust/crontab-rs/Cargo.toml --locked` | 0.124071 | 0 |

Initial process-test failures identify fixture NUL-path, redirected-stderr, lifetime and executable-write race errors; those fixtures are corrected without relaxing assertions. The first differential/benchmark attempts identify real cgo SIGPIPE termination, fixed with the thread-local mask. The first lint gate identifies one collapsible conditional. Recording-staleness failures precede final byte-identical re-recording. Early coverage attempts identify Go package-init coverage-hook timing and a directory-reader setup error; the final aggregate uses actual covered test subprocesses.

Retained summary/log evidence is in ignored `.state/crontab-evidence-20261125/`. It contains no claim or session identity. The immutable recording SHA-256 is `1b955f90830e85e6eac6ac499490f7daecc41cdeb017e00102da55f91f06227f`.
