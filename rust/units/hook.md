---
created_on: 2026-11-25 08:46
last_modified: 2026-11-25 09:04
status: current
---

# Hook boundary inventory

This inventory is for maintainers integrating the native Rust CLI. `GenerateHookScript` has a native implementation and differential evidence. `HandleHookExport` remains incomplete: this crate contains no implementation, replacement status/discovery fixture, or adapter claiming to cover it. Application integration and native acceptance on every supported platform remain separate work.

## Source APIs and side effects

The complete production boundary is [internal/hook/hook.go](../../internal/hook/hook.go).

| Source API | Inputs and outputs | Observations and side effects | Rust coverage |
| :--- | :--- | :--- | :--- |
| `GenerateHookScript(shellName, binName string) (string, error)` | Exact lowercase `bash`, `zsh`, or `fish`; empty binary selects `cron-person`; returns the literal shell script including final newline. Unsupported shells return an empty script and `unsupported shell %q: supported shells are bash, zsh, fish`. | Pure generation: no environment lookup, filesystem access, output write, subprocess, store access, or persistent mutation. Go strings admit arbitrary bytes. `%q` uses Go's Unicode 15 printability and escapes invalid UTF-8 byte by byte. | Native `port::generate(&[u8], &[u8]) -> Result<Vec<u8>, String>`; both reference adapters call the actual production Go function. |
| `HandleHookExport(st *store.Store, dir string, isAgent bool, stderr io.Writer) error` | Uses the supplied real store, starting directory, output mode, and writer. Returns discovery, status, or output errors without wrapping them. | Discovers an ancestor `.cronrc`, inspects its authorization status and raw contents through the store, formats its display path, and may write one warning. Store inspection reloads its in-memory records. Does not authorize, install a crontab, run tasks, record history, or persist a store mutation. | Incomplete. Requires the real native store, cronrc discovery/read behavior, display path formatting, and writer/error integration described below. |

### Generated shell behavior

All three templates insert binary bytes verbatim between the original double quotes and invoke `hook export <shell>`. They do not escape quotes, backticks, dollar substitutions, percent signs, newlines, NUL, or invalid UTF-8. Shell parsing can therefore expand or reinterpret a binary name. This existing behavior is preserved; a correction requires a separate user decision.

| Shell | Registration and execution contract |
| :--- | :--- |
| bash | Defines `_cron_person_hook`, saves `$?`, invokes the binary, and returns the saved status. Prepends `_cron_person_hook` to `PROMPT_COMMAND` unless the semicolon-delimited membership check already finds it. Preserves existing prompt text. |
| zsh | Defines `_cron_person_hook` with the same status contract. Declares `precmd_functions` and `chpwd_functions` arrays and prepends the function only when it is absent. Preserves other callbacks. |
| fish | Defines `__cron_person_export_eval --on-event fish_prompt`, saves `$status`, invokes the binary, and returns the saved status. Sourcing the definition again replaces the same named function. |

The scripts inherit the caller's execution environment and working directory when executed. Binary output streams pass through; no `eval`, warning suppression, or trap replacement is added. Registration and function definitions are the shell state changes, while command execution occurs when the registered function runs. A default binary resolves through the shell's normal command lookup.

### Incomplete export behavior and integration requirements

The exact production sequence is:

1. `cronrc.FindCronrc(dir)` resolves `filepath.Abs`, walks the directory and its ancestors through the filesystem root, and returns the first `.cronrc` whose followed `os.Stat` mode is regular. Stat failures and nonregular entries are skipped; symlinks to regular files qualify. No match returns success without output or store access. A path-resolution error, including relative discovery from a removed working directory, is returned. Special files must not be opened or block discovery.
2. For a match, call `st.Status(filepath.Dir(foundCronrc))` on the actual authorization store. `Status` delegates to `Inspect`: resolve the directory, read the real `.cronrc` snapshot, lock and reload store records, and compare the raw-byte hash with the record. The snapshot, record, and hashing details must come from the store/cronrc implementations, without a fabricated status. The returned record is ignored by this hook. Return any error before path formatting or output.
3. Apply `display.Path` only after successful status inspection. It consults `os.UserHomeDir` (`HOME` on Unix, `USERPROFILE` on Windows), cleans the home directory, and folds an exact home or separator-bounded home prefix to `~`. On a home lookup error it preserves the path. Do not resolve symlinks or use the display path for filesystem/store access.
4. Write the exact line below for unauthorized or changed status and return the `fmt.Fprintf` writer error. Allowed, missing-cronrc, orphan, and unrecognized statuses produce no output and return success. Preserve partial-write/error behavior through the real native writer contract; do not swallow output errors.

| Status | Agent mode output | Human output |
| :--- | :--- | :--- |
| Unauthorized | `WARN: unauthorized .cronrc at <display-path>\n` | `[WARN] cron-person: unauthorized .cronrc detected at <display-path> (run 'cron-person allow' to approve)\n` |
| Changed | `WARN: modified .cronrc at <display-path>\n` | `[WARN] cron-person: modified .cronrc detected at <display-path> (run 'cron-person allow' to approve changes)\n` |

The strings above use `\n` to denote one newline. Paths are inserted as raw string bytes, without quoting. Missing/nonregular files can arise between discovery and status inspection and remain quiet according to the store classification. Read errors and store reload errors remain errors. Store reload can update the in-memory view even when the eventual status is quiet.

Completion requires a genuine Go reference adapter around `HandleHookExport`, native integration with the production Rust store/cronrc/display boundaries, and cases covering both output modes; unauthorized, allowed, changed, missing, and orphan states; ancestor discovery and home folding; symlinks and nonregular/FIFO entries; removed working directories; store/read failures; and failing/partial writers. Fixtures must bound ancestor discovery. Replay those cases through the actual CLI `hook export` path, preserve arbitrary path bytes where the OS supports them, and rerun the source tests. Do not mark the whole hook boundary complete based on generation-only evidence.

## Native and bridge design

`src/entry.rs` is the library entry point. With default features disabled it exposes only the native `port` module and does not compile the generated bridge or its C exports. Its only normal dependency is `unicode-general-category = 0.6.0`, pinned to Unicode 15.0.0 to match the reference Go toolchain's `strconv.IsPrint`. Generated serialization, JSON, and hex dependencies are optional under `bridge`; tests have their own dev dependencies.

The default `bridge` feature includes generated `src/lib.rs` and `src/wire.rs` for migration tooling. The native implementation forbids unsafe code. The gate's 22 audited unsafe uses belong to the generated FFI boundary, not the native generator. The two bridge exports are:

| Export | Purpose |
| :--- | :--- |
| `GenerateHookScript` | Direct UTF-8 JSON boundary calling production Go / native Rust generation. |
| `GenerateHookBytes` | Decode shell/binary hex, call the same actual implementation, and encode script bytes as hex. Invalid shell hex takes precedence over invalid binary hex; errors are exactly `invalid shell hex` and `invalid binary hex`. Original unsupported-shell errors pass through unchanged. |

## Evidence

Validation on Linux uses Go 1.26.11 and Rust 1.100.0. The recorded corpus is deterministic: 468 cases, including 440 errors. It covers all supported shells, default/custom binary names, whitespace and metacharacters, every byte value, NUL and malformed UTF-8, long binary names, Unicode categories and version boundaries, exact Go control/Unicode quoting, invalid hex, and fixed-seed arbitrary byte shell names.

The Go original is recorded twice before the Rust implementation is supplied; both outputs compare byte-for-byte. `recorded-from.json` fingerprints the reference directory, whose adapters import the production package. The fingerprint does not include transitive production imports, so matching metadata alone is not proof that those imports are unchanged.

| Check | Result and scope |
| :--- | :--- |
| Original Go tests | `go test -race -coverprofile=... ./rust/reference/hook ./internal/hook` passes; reference 100.0% statements, original hook 96.6%. Includes all original hook tests. Fish execution skips because fish is absent. |
| Build and differential replay | `rust-anything build` succeeds; `diff` and the gate match 468/468 recorded cases. |
| Gate | All 12 checks pass without changing limits or goldens: fmt, clippy, tests, unsafe review, lockfile, ported, size, error cases, panic handling, build, API, and behavior. |
| Size measurement limitation | The gate reports 139 Rust lines against 59 Go reference lines (2.4x). The Go denominator is the adapter/reference directory, not imported production code. This is the current tooling measurement, not a whole-production size comparison. No code is padded or relocated to affect it. |
| Native configuration | `cargo test --locked --no-default-features` and clippy over all targets pass. `cargo tree --no-default-features -e normal` lists only the Unicode category dependency. The native debug shared library has no defined dynamic C exports (`nm -D --defined-only`). |
| Rust coverage | Integration-only instrumented execution covers 71/72 native implementation lines (98.61%) and 7/7 functions (100%). Generated `src/lib.rs`, `src/wire.rs`, and test files are excluded. `src/entry.rs` contains no executable lines. The uncovered line converts an impossible invalid-UTF-8 result from valid UTF-8 inputs into an error; no coverage suppression is added. |
| Native shell execution | Actual bash and zsh execute native-generated scripts, with 36 assertions across explicit paths containing spaces, default lookup, and preserved variable expansion. Checks include repeated registration, existing callbacks and INT traps, previous success/failure statuses, exact forwarded arguments, and stderr passthrough even when the invoked process exits 23. The test executable is a transport stub; it provides no store/discovery result and does not validate native export behavior. Fish is not installed. |
| Shadow | All 468 cases pass through the generated Go wrapper in old, shadow, and new modes; shadow records zero mismatches. |
| Go static analysis | `go vet ./rust/reference/hook ./internal/hook` passes. |

The measured bridge costs are:

| Function | Cases | Original ns/call | Rust bridge ns/call | Original/Rust speed ratio |
| :--- | ---: | ---: | ---: | ---: |
| `GenerateHookScript` | 40 | 172.40 | 426.10 | 0.405 |
| `GenerateHookBytes` | 428 | 427.46 | 1931.10 | 0.221 |

These are JSON/FFI migration bridge measurements, not direct native Rust timings or application latency claims. The bridge is slower in both measurements.

No all-platform acceptance, fish runtime execution or native `HandleHookExport` is claimed. The original shell quoting behavior is preserved.

## Application integration

The Rust entry point calls native `port::generate` for bash/zsh/fish generation, including supported `--` separation. It uses this crate with default features disabled. Help, export, unsupported shells and other argument forms still run the Go original. `RUST_ANYTHING_UNIT_HOOK=original` disables native dispatch. The fixture wraps the entire application rather than its fallback; native paths now receive the same filesystem/stream observations as Go paths.

Application process tests pass exact script bytes, explicit native/fallback selection, the kill switch, real failed stdout writes across eight agent-mode values, and actual Unix SIGPIPE termination. The signal restoration is isolated at the fatal output boundary with a safety comment. Application coverage passes at 92.08% lines and 100% functions. Full CLI replay passes 556/556 with 553 Go fallbacks, versus all 556 with the kill switch. HTTP remains 337/337 through Go. These results cover generation integration only.

Six 120-column, 22-row terminal captures are retained under ignored `.state/rust-hook-ui/`: before/after bash, zsh and fish. The parent inspected all six; an independent reviewer, given only captures and visual rules, found no clipping, readability defect or visible difference. Screenshots demonstrate output rendering; the separate execution harness establishes shell behavior. Final whole-application UI acceptance remains open.

## Reproduce validation

Run from the repository root. Recording replaces goldens and is not a normal verification step; retain the existing cases when checking this implementation.

```sh
rust-anything build rust/hook-rs
rust-anything diff rust/hook-rs
rust-anything bench rust/hook-rs --json
rust-anything gate rust/hook-rs --json
cargo test --manifest-path rust/hook-rs/Cargo.toml --locked --no-default-features
cargo clippy --manifest-path rust/hook-rs/Cargo.toml --locked --all-targets --no-default-features -- -D warnings
cargo tree --manifest-path rust/hook-rs/Cargo.toml --locked --no-default-features -e normal
go test -race ./rust/reference/hook ./internal/hook
go vet ./rust/reference/hook ./internal/hook
RUST_ANYTHING_MODE=old go -C rust/hook-rs/go test -count=1 ./shadow
RUST_ANYTHING_MODE=shadow go -C rust/hook-rs/go test -count=1 ./shadow
RUST_ANYTHING_MODE=new go -C rust/hook-rs/go test -count=1 ./shadow
```

Native shell tests launch Python through `uv run --no-project python` from the repository root and use its local `.venv`. Provision that environment with `uv venv .venv` when absent; do not replace an existing environment. The available bash/zsh execution harness still passes all 36 assertions after this launcher change (0.29 s command time).

For a coverage report, choose an invocation-owned ignored output path, then run:

```sh
cargo llvm-cov --manifest-path rust/hook-rs/Cargo.toml --locked \
  --test contracts --test native_shells \
  --ignore-filename-regex 'src/(lib|wire)\.rs|/tests/' \
  --fail-under-lines 90 --fail-under-functions 90 --json \
  --output-path .state/rust-hook-coverage.json
cargo llvm-cov clean --manifest-path rust/hook-rs/Cargo.toml --locked
```

The coverage cleanup removes this crate's coverage artifacts; preserve normal incremental and shared caches. The completed run retains approximately 189 MiB of crate/cache artifacts, below the 600 MB task budget, and removes the dedicated coverage build tree after exporting its report. Temporary shell fixtures and owned process groups are removed by the test harness. Application-level and other workers' resources are outside this unit's cleanup scope.

## Exact source file inventory

| Path | Role |
| :--- | :--- |
| `rust/hook-rs/.gitignore` | Exclude generated binaries and migration caches. |
| `rust/hook-rs/Cargo.toml` | Native/bridge features, pinned Unicode data, tests and library entry point. |
| `rust/hook-rs/Cargo.lock` | Locked dependencies. |
| `rust/hook-rs/README.md` | Native consumer documentation. |
| `rust/hook-rs/cases.jsonl` | Unmodified original recordings. |
| `rust/hook-rs/recorded-from.json` | Reference source fingerprint. |
| `rust/hook-rs/rust-anything.json` | Generated bridge API manifest. |
| `rust/hook-rs/src/entry.rs` | Feature isolation for native consumers. |
| `rust/hook-rs/src/lib.rs` | Generated optional FFI/JSON bridge. |
| `rust/hook-rs/src/wire.rs` | Generated optional wire helpers. |
| `rust/hook-rs/src/port.rs` | Actual byte-preserving native generator and Go diagnostic quoting. |
| `rust/hook-rs/tests/contracts.rs` | Replay original corpus through native and bridge APIs. |
| `rust/hook-rs/tests/native_shells.rs` | Generate scripts natively and own the temporary fixture. |
| `rust/hook-rs/tests/shell_contracts.py` | Execute available native shells and reap owned processes. |
| `rust/hook-rs/go/go.mod` | Go bridge module importing the production repository. |
| `rust/hook-rs/go/go.sum` | Go dependency integrity metadata. |
| `rust/hook-rs/go/hookreference.go` | Generated differential Go wrapper. |
| `rust/hook-rs/go/shadow/hookreference.go` | Generated old/shadow/new Go wrapper. |
| `rust/hook-rs/go/shadow/hookreference_test.go` | Replay all corpus cases through shadow modes. |
| `rust/reference/hook/hook.go` | Genuine production Go adapters, including byte-safe hex boundary. |
| `rust/reference/hook/hook_test.go` | Reference error/success and byte-preservation tests. |
| `rust/reference/hook-inputs.py` | Deterministic corpus generator. |
| `rust/reference/hook-inputs.jsonl` | Corpus inputs. |
| `rust/units/hook.md` | This inventory, evidence, omissions and integration contract. |

Private claim receipts, session identity, command logs, raw coverage and benchmark outputs, resource accounting, and resumable handoffs live only in ignored `.state/rust-hook-*` files or the private tracker. The inventory above covers the delegated generator delivery; parent application glue, fixture integration, process tests and shared ledgers are additional parent-owned work.
