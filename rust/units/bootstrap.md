---
created_on: 2026-11-25 09:50
last_modified: 2026-11-25 10:31
status: current
---

# Development bootstrap helper

This ledger gives the integrating maintainer the implementation boundary, evidence and remaining work for `scripts/dev-bootstrap.go`. The isolated Linux library/helper passes all twelve migration gates and matches 169 genuine Go observations. Windows behavior and native macOS/Windows acceptance remain unfinished. Root recipes still use Go; no installed override or application wiring changes belong to this delivery.

## Complete source API inventory

The source consists of four functions; no source function is omitted from the Linux implementation. The reference builds the actual source. To reach its private functions, it copies that source into invocation-owned build scratch, renames only `main` to `originalMain`, and compiles the data-only probe alongside it. It never reimplements bootstrap decisions in Go.

| Original API | Rust API | Behavior and side effects |
| --- | --- | --- |
| `commandOutput(dir, exe, args...)` | `bootstrap::command_output(&Path, &OsStr, &[OsString]) -> Result<Vec<u8>, Error>` | Inherited environment, null stdin, captured output, directory-specific PWD, original argv[0], PATH lookup before child chdir. Trims successful stdout with Go-compatible Unicode whitespace. On process failure, preserves stderr bytes, Go's 32 KiB prefix/suffix capture and omitted-byte count, exit/signal status and diagnostic context. Lookup, chdir, NUL and spawn failures retain their observed precedence. |
| `bootstrap(source, dotfiles)` | `bootstrap::bootstrap(&Path, &Path) -> Result<Vec<u8>, Error>` | Lexically resolves dotfiles first; executes `dotfiles tool dev use --help` there and checks for the `--bin` substring. Resolves/stats the source, rejects an empty file, resolves the repository cwd, runs the source's `--version`, registers the exact bin/source-repo/version arguments, then asks `dotfiles tool which cron-person --bin`. Help failure becomes the original generic capability error. Registration failure has its original context. Successful registration persists if the final lookup fails; there is no rollback. |
| `run()` | `bootstrap::run(&mut impl Write) -> Result<(), Error>` | Requires nonempty `dotfiles_dir`, uses `bin/cron-person`, registers it, and prints the exact success line. Writer errors are ignored as in the original. |
| `main()` | `bootstrap::main_status(stdout, stderr) -> i32`, `bootstrap::process_main() -> i32`, `dev-bootstrap` binary | Ignores arguments. Writes an error plus LF to stderr and returns 1, otherwise 0. The actual standard-stream wrapper reproduces Go's SIGPIPE termination for closed stdout/stderr on Unix. Generic library writers retain `run`/main's ignored-write-error behavior. |

`Error.0` is the authoritative byte diagnostic; it does not promise UTF-8. Unix paths remain OS byte strings. Absolute-path cleaning does not canonicalize symlinks. A valid symbolic PWD naming the current inode is retained. Lookup uses effective-ID executable access, excludes directories, and honors the last `GODEBUG=execerrdot` setting. Unicode printability is pinned to Unicode 15 through `unicode-general-category = 0.6.0`, matching the inspected Go toolchain.

The helper itself delegates override and managed-release policy to dotfiles. The fake CLI records exact call order, argv, cwd, HOME and PATH; preserves a managed release containing NUL/invalid UTF-8; copies the source into a fixture override; and records registration arguments. Observations include exact stdout, stderr, status, return/error bytes, trace, release, registration, override and printed output. Only the random fixture-root prefix is normalized. Every tool invocation uses fake source/dotfiles executables inside system-temp fixtures, an isolated HOME/PATH/cwd, and absolute shell utilities. No real dotfiles CLI, override or release is used.

## Source paths and consumption

| Paths | Purpose |
| --- | --- |
| `rust/bootstrap-rs/src/bootstrap.rs`, `quote.rs`, `main.rs` | Native process/path/error behavior, Go quoting, standalone entry point. |
| `rust/bootstrap-rs/src/entry.rs` | Native module and optional bridge boundary. |
| `rust/bootstrap-rs/src/port.rs`, `probe.rs` | Test-only scalar adapter and actual Rust API probe. The adapter launches the genuine Go fixture driver with the actual Rust target; the driver supplies fixtures, not a Go fallback implementation. |
| `rust/bootstrap-rs/src/lib.rs`, `wire.rs` | Generated FFI; 15 unsafe uses retain SAFETY comments. |
| `rust/bootstrap-rs/tests/native.rs`, `process.rs` | Direct native process/failure checks, repeated original comparison, full recorded replay and actual FFI invocation. |
| `rust/bootstrap-rs/Cargo.toml`, `Cargo.lock`, `.gitignore`, `README.md` | Standalone crate, pinned dependencies and generated crate support. Rust 1.100 is the compiler used here; no older MSRV is asserted. |
| `rust/bootstrap-rs/cases.jsonl`, `recorded-from.json`, `rust-anything.json` | Immutable original observations, original reference fingerprint and current export metadata. |
| `rust/bootstrap-rs/go/go.mod`, `go/bootstrapreference.go`, `go/shadow/bootstrapreference.go`, `go/shadow/corpus_test.go` | Generated cgo bridge/shadow and complete shadow replay. |
| `rust/reference/bootstrap/bootstrap.go`, `process_unix.go`, `process_windows.go`, `driver/main.go` | Isolated fixture orchestration, bounded child execution and driver entry point. |
| `rust/reference/bootstrap/bootstrap_test.go` | Normal own-package Go coverage: actual-source builds/replay, protocol/IO failures, fixture validation and real cancellation/reaping. |
| `rust/reference/bootstrap/probe.go.txt` | Data-only private-function probe compiled alongside the actual source in scratch. It is not a separately compiled root Go package. |
| `rust/reference/bootstrap-tools.py`, `bootstrap-inputs.jsonl` | Offline build/validation entry point and 169 explicit inputs. |
| `rust/units/bootstrap.md` | This ledger. |

Native consumption is `bootstrap-rs = { path = "../bootstrap-rs", default-features = false }`. The native executable/library then compile without Go fixture, JSON or FFI support. The separate default bridge feature is validation infrastructure. No app wiring, root default-build change or installed development override is supplied.

## Characterization and validation

Two original recordings precede native implementation: 157 observations each, byte-identical after inspection (1.016 s and 0.916 s), with SHA-256 `a0805f964b9808ed5626612e08dfa8fe79d3386637112f4776bf5f59244845eb`. They contain 100 real helper failures and 57 successes, encoded as observations rather than adapter `Err` results. Parent integration appends twelve malformed protocol/fixture inputs and records actual Go twice (0.916 s and 0.816 s). Both 169-case recordings agree byte-for-byte, and the first 157 observations remain byte-identical. No expected result is rewritten. Cases cover command failures, malformed executables, missing/empty sources, source/dotfiles paths, Unicode/raw output, NUL arguments, help capability errors, missing CLI, symlinks, removed cwd, existing overrides, preserved/absent releases, registration/lookup failures and failure precedence.

| Check | Result | Wall time |
| --- | --- | --- |
| `rust-anything build rust/bootstrap-rs` | Actual shared library built | 3.872 s |
| Release binaries, offline/locked | Actual native helper/probe built | 4.974 s |
| `rust-anything diff ... --json` after parent recording refresh | 169/169 matched | 1.717 s |
| `cargo test ... --offline --locked` | 7 passed inside the final gate; 47 supplemental Linux scenarios, each original repeated twice; all 169 corpus observations and actual bridge calls | Included in final gate |
| `cargo llvm-cov ... --json` | First-party 427/448 lines (95.31%), 51/55 functions (92.73%); native implementation 351/365 lines (96.16%), 43/46 functions (93.48%) | 5.877 s |
| Normal `go test -race -count=1 -coverprofile=... ./rust/reference/bootstrap/...` after corpus/test refresh | Reference package 175/190 statements (92.1%); empty driver has zero statements | 2.569 s |
| Unchanged root `go tool go-test-coverage --config=.testcoverage.yml --profile=...` on that normal profile | Package threshold 90% passes; no exclusions or threshold changes | 0.665 s |
| Original `go test -race ... ./scripts` | Pass, 90.7% statements | 1.468 s |
| `go vet ./scripts ./rust/reference/bootstrap/...` | Pass | 0.164 s |
| Shadow old / shadow / new, refreshed full corpus | 169 each; shadow 169 calls, zero mismatches | 1.320 / 2.619 / 2.322 s |
| `rust-anything bench ... --json` | Original 4,334,101 ns; Rust bridge 9,943,813 ns; speedup 0.435859 | 4.823 s |
| Native-only `cargo test --no-default-features` | Compiles; zero tests selected | 0.415 s |
| Native-only aarch64 macOS / x86_64 Windows GNU `cargo check` | Both compile; no native execution evidence | 1.167 / 0.916 s |
| Final mandatory gate after parent recording/test and temporary-directory repair | All 12 checks pass; 169/169 behavior, fresh production-adapter fingerprint | 5.032 s |
| Root `just test`, `just vet`, `just coverage` | All pass; race-enabled package coverage satisfies unchanged thresholds | 32.713 / 3.579 / 60.097 s |

LLVM totals exclude generated `lib.rs`, `wire.rs` and declaration-only `entry.rs`. The first-party total includes one in-file adapter unit test; the separately reported native total contains no test code. Generated FFI is exercised through cgo/shadow. Test/probe instrumentation is not substituted for native coverage. The Go own-package metric comes directly from its ordinary race/coverage run, not a custom aggregation tool. Root coverage also passes in the integrating checkout; this does not establish native platform acceptance.

The benchmark measures the complete fixture/FFI boundary, including the extra fixture-driver process on the Rust side. It is 2.29 times slower at this boundary and does not establish native-helper latency or a performance gain.

Native process checks additionally cover Go quoting, PATH/ErrDot/GODEBUG, child cwd versus lookup cwd, permission/format/interpreter errors, non-UTF-8/symbolic paths, stderr capture boundaries, terminating signals including Linux realtime signal 34, and closed standard streams. Core dumps are disabled in fixtures. Child contexts and invocation process groups bound test execution; compiler scratch is a unique repository `.tmp` child, while fake-tool fixtures live in system temp. All setup/cleanup errors are checked. Tests are offline. Normal incremental state is retained; dedicated coverage build artifacts are removed only after consuming/exporting their counters.

## Parent repairs and remaining native limits

The original helper failures are values containing exact diagnostics/status/effects; invalid fixture/protocol inputs return adapter errors. The earlier gate rejected missing recorded adapter errors and a stale directory fingerprint. Parent integration added genuine original error observations, refreshed provenance twice and corrected both Go and Rust corpus tests to compare exact error envelopes as well as successful observations. An initial Rust corpus test incorrectly assumed every recorded input was valid scenario JSON; that test assumption was fixed without changing any expected output. Generated shadow replay now checks the same errors in old, shadow and new modes.

The portable manifest selects only `rust/reference/bootstrap/bootstrap.go`, excluding adapter tests from size credit. The installed gate counts 602 Rust lines against 319 production adapter lines (1.9x), within the unchanged three-times-plus-50 limit. The original helper is 73 analyzed source lines. Process fixture helpers remain compilation dependencies; a single-file fingerprint does not inventory every original dependency. No source or test code was padded for this metric.

Actual Go and Rust standalone helper output was captured in matching 100-column by 8-row PTYs for success, missing recipe environment and unsupported tool capability. All three before/after pairs are text-identical, inspected and independently reviewed without visible defects. These stills establish appearance; recorded/process tests establish exit status and effects. Fake tools and a stable fixture destination were used, with no installed override changes; owned fixtures were removed.

Windows command lookup, PATHEXT/implicit-current-directory behavior, drive/root-relative path resolution, UTF-16 and native diagnostic formatting remain incomplete. The non-Unix fallback compiles but is not accepted as parity. Shell fixture tests are Unix-only, and the reference returns an explicit unsupported-fixture error on Windows. macOS compiles but its native filesystem/process/signal behavior has not run here. Other Unix systems use the Darwin signal table and are outside demonstrated parity. Release acceptance still needs the applicable six native targets; compilation is not native validation.

Not forced here: inherited environment/PWD races in a multithreaded embedding host; process/resource exhaustion; privilege-specific executable access; a real core-dump flag; stopped-child job control; filesystem races between stat/lookup/spawn; write failures other than EPIPE on actual standard streams. The Linux contracts above are demonstrated by the finite fixtures, not claims about every kernel error. The original helper permits a called executable to hang; the library does not add a production timeout.

## Reproduction and handoff

Run from repository root with its existing `.venv`. Set `BOOTSTRAP_EVIDENCE` to an invocation-owned `.state` directory. Set `BOOTSTRAP_GO_BIN` only when selecting an already installed compiler; no toolchain fetch is permitted. `prepare` exports the actual Go main/probe/fixture driver. Validation commands are passed to the bounded runner so fake targets and evidence paths are supplied:

```sh
uv run --no-project python rust/reference/bootstrap-tools.py prepare
uv run --no-project python rust/reference/bootstrap-tools.py run build rust-anything build rust/bootstrap-rs
uv run --no-project python rust/reference/bootstrap-tools.py run bins cargo build --manifest-path rust/bootstrap-rs/Cargo.toml --offline --locked --release --bins
uv run --no-project python rust/reference/bootstrap-tools.py run diff rust-anything diff rust/bootstrap-rs --json
uv run --no-project python rust/reference/bootstrap-tools.py run native cargo test --manifest-path rust/bootstrap-rs/Cargo.toml --offline --locked
uv run --no-project python rust/reference/bootstrap-tools.py coverage
uv run --no-project python rust/reference/bootstrap-tools.py run own go test -race -count=1 -coverprofile="$BOOTSTRAP_EVIDENCE/reference.cover" ./rust/reference/bootstrap/...
uv run --no-project python rust/reference/bootstrap-tools.py run threshold go tool go-test-coverage --config=.testcoverage.yml --profile="$BOOTSTRAP_EVIDENCE/reference.cover"
uv run --no-project python rust/reference/bootstrap-tools.py run bench rust-anything bench rust/bootstrap-rs --json
uv run --no-project python rust/reference/bootstrap-tools.py run gate rust-anything gate rust/bootstrap-rs --json
```

The runner records argv, exit status, wall time and output. Deliberate recording refresh uses `rust-anything record rust/bootstrap-rs --count 0 --inputs rust/reference/bootstrap-inputs.jsonl` through the same runner; repeat twice and verify existing observations before accepting source-provenance changes. Normal validation does not rerecord. Native platform acceptance and switching the development recipe remain separate from this gated Linux helper delivery.
