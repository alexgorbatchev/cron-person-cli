---
created_on: 2026-11-25 08:01
last_modified: 2026-11-25 09:11
status: current
---

# Filelock migration ledger

This is the implementation and validation record for the parent integrating the
native lock library. Implementation and Linux unit validation are published in
`db231f9`. Complete acceptance remains open: this Linux host cannot execute
the required native macOS and Windows tests.

## Ownership and checkpoint

The completed implementation handoff covers `rust/reference/filelock*`,
`rust/filelock-rs/**`, this ledger and ignored `.state/rust-filelock-*` reports.
App wiring and complete platform acceptance remain with the parent.
Private ownership and synchronization records remain in the task tracker and
ignored local state; they are excluded from published source.

## Source API and side-effect inventory

Source: `internal/filelock/{filelock,lock_unix,lock_windows}.go`. The source has
no exported types or methods. Every operation has a native Rust implementation.
The generated bridge exports only test-only `Scenario`; its API gate does not
replace this inventory.

| Source contract | Rust implementation | Evidence / limit |
| --- | --- | --- |
| `ErrLocked` | `Error::Locked`, exact `file is locked` text | Genuine replay and native contention; other errno values remain distinct. |
| `Lock(*os.File)` | `lock(&LockFile)` | Safe Unix `File::lock`; Windows one-byte LockFileEx. Native blocking, closed-file and Linux O_PATH failure tests. |
| `TryLock(*os.File)` | `try_lock(&LockFile)` | Only Unix WouldBlock / Windows ERROR_LOCK_VIOLATION becomes contention. Real fresh-open probes. |
| `Unlock(*os.File)` | `unlock(&LockFile)` | Native unlock; Unix cloned/inherited descriptions share lock ownership. |
| `LockPath(path)` | `lock_path(path) -> PathLock` | Opens read/write, creates 0600, never truncates, blocks, retains pathname/inode/content. Acquisition failure joins lock and close failures. |
| Returned release callback | `PathLock::release(&mut self)` | Unlock before fallible close; ordered joined errors. Repeated release retains both native errors. |
| Internal Windows `lockFileEx` | Windows `platform::lock` | Zero-offset OVERLAPPED, exactly one byte, matching unlock range; source verified, native runtime pending. |
| File ownership | `LockFile::{open,from_file,file,path,close}` | Owns live/closed states, no invalid Rust File. `from_file` transfers ownership. Explicit close reports errors. |
| Error chains and joined text | Typed `Error`, `ReleaseError(Vec<Error>)` | Raw errno, operation/path, closed classification, source and ordered leaves. `error_bytes()` preserves non-UTF-8 Unix paths and joined newlines. `Display` is necessarily Unicode text. |
| Disposal fallback | `Drop for PathLock` | Best-effort release; parent retains the guard and calls explicit release for reportable errors. |

Installed Rust 1.100.0 local rustdoc source `src/std/sys/fs/unix.rs.html`, lock
operations at line 1323 onward, uses flock with the required flags and no EINTR
retry. Explicit close needs one small unsafe libc call because File Drop loses
close errors; the consumed descriptor closes once and is never retried on EINTR.

Windows std locks a different range, so the port uses actual resolved
windows-sys 0.61.2 bindings. Their declarations and installed Go 1.26.11 syscall
source are inspected. Open sharing is READ | WRITE, excluding DELETE, with
noninheritable handles. Error formatting follows Go's English-first
FormatMessageW, 300-character buffer, CR/LF trimming and synthetic EINVAL value.
Windows execution remains pending.

## Genuine adapters and native evidence

The Go adapter calls the actual production Go filelock package; the Rust adapter
calls the actual native Rust implementation. Neither synthesizes expected lock
results. Only invocation fixture paths are normalized. A hex representation
transports real non-UTF-8 Go error bytes without JSON replacing them. Unknown
scenarios exercise genuine adapter errors.

The native Rust test builds a Go helper from the reference test package and runs
the Python harness with that helper and the real Rust probe. Process I/O
handshakes have five-second deadlines; an 80ms negative observation proves
blocking. Each child/pipe/fixture has cleanup ownership, including descendants
in invocation-owned Unix process groups. No PID determines lock liveness.

Linux passes 116 assertions across Go/Go, Go/Rust, Rust/Go and Rust/Rust pairs:

- Contention, blocking acquisition, explicit unlock and close.
- Graceful holder exit and abrupt kill, followed by successful fresh acquisition.
- Default close-on-exec while a child remains alive.
- Deliberate Unix inheritance: original close retains the child's lock; explicit
  unlock releases the shared description while the child remains alive.
- Stable inode/path, no truncation or content changes, 0600 creation,
  missing-parent and directory errors.

Contract tests add adopted read-only files, closed handles, repeated release,
guard Drop, Rust descriptor clones, NUL paths, non-UTF-8 error bytes, and live
Linux O_PATH descriptors returning real EBADF for lock/try-lock/unlock.
Windows-only harness branches check byte one remains readable while byte zero
is locked and that rename/delete is denied under a live lock. Those branches
are prepared but unexecuted here.

## Checks and durations

Host: Linux 6.8.0-149-generic x86_64. System `/tmp` fixtures use ext4, verified
with `findmnt -T /tmp -n -o FSTYPE`. Toolchains: Go 1.26.11 and Rust
1.100.0. The crate declares the tested Rust 1.100 minimum; it removes the
scaffold's unverified 1.97 claim.

Full commands, durations and exits, including fixed failed attempts, are in
`.state/rust-filelock-timings.jsonl`. Logs are
`.state/rust-filelock-<label>.log`. Final passing runs:

| Label | Command / result | Wall time |
| --- | --- | --- |
| baseline | `go test -race -count=1 -cover ./internal/filelock`: 92.3% statements | 1.412s |
| bridge | `rust-anything bridge . rust/reference/filelock/ --name filelock-rs` | 0.132s |
| build | `rust-anything build rust/filelock-rs`: release shared library; final gate rebuilds final source | 4.356s |
| record-final | `rust-anything record rust/filelock-rs --count 30 --inputs rust/reference/filelock-inputs.jsonl`: 62 cases, 45 thrown validation cases | 0.295s |
| gate-consumer | `rust-anything gate rust/filelock-rs --json`: all 12 checks, including fmt/clippy/tests/build and 62/62 behavior | 6.340s |
| diff-consumer | `rust-anything diff rust/filelock-rs --json`: 62/62, zero missing/mismatches | 0.102s |
| bench-consumer | `rust-anything bench rust/filelock-rs --min-ms 200 --json`: Go 21,756.62ns, Rust 20,344.73ns, 1.0694x | 0.618s |
| go-coverage-final | `go test -race -count=1 -coverprofile=.state/rust-filelock-go.cover ./internal/filelock ./rust/reference/filelock`: source 92.3%, adapter 91.9% | 1.376s |
| vet-final | `go vet ./internal/filelock ./rust/reference/filelock` | 0.105s |
| coverage-consumer | `cargo llvm-cov --manifest-path rust/filelock-rs/Cargo.toml --locked` with generated/test exclusions, 90% line/function thresholds and exported JSON | 2.588s |
| plain-test | `cargo test --manifest-path rust/filelock-rs/Cargo.toml --locked --no-default-features`: 4 contract tests plus native suite; harness 1.536s | 2.147s |
| plain-build | `cargo build --manifest-path rust/filelock-rs/Cargo.toml --locked --no-default-features --lib` | 0.025s |
| plain-tree | `cargo tree --manifest-path rust/filelock-rs/Cargo.toml --locked --no-default-features --edges normal`: only libc on Linux | 0.025s |
| windows-check-consumer | `cargo check --locked --manifest-path rust/filelock-rs/Cargo.toml --target x86_64-pc-windows-gnu --all-targets --all-features` | 0.241s |
| darwin-check-consumer | Same check with `--target aarch64-apple-darwin` | 0.450s |

The benchmark measures scenario dispatch, filesystem fixtures and validation
errors, including 45 adapter failures. It is not pure lock throughput or
application/capacity acceptance.

Coverage is 278/289 lines (96.19%), 41/43 functions (95.35%), 335/368 regions
(91.03%). Production filelock.rs is 154/159 lines (96.86%) and 31/31 functions
(100%); adapter port.rs is 124/130 lines (95.38%). Exclusions are only generated
`src/lib.rs`, generated `src/wire.rs` and tests. Windows cfg code is not executed
or included in Linux coverage. The final coverage run reports 116 native
assertions in 1.540s (1.78s test including helper compilation).

The native process launcher uses `uv run --no-project python` from the repository
root and its local `.venv`. Provision a missing environment with `uv venv .venv`;
preserve an existing environment. The launcher change preserves all 116 native
assertions (1.568 s harness; 2.44 s Cargo command). No library behavior changes.

Exact coverage reproduction, with the crate argument before the shared options:

```sh
cargo llvm-cov --manifest-path rust/filelock-rs/Cargo.toml --locked \
  --ignore-filename-regex 'src/(lib|wire)\.rs|tests/' \
  --fail-under-lines 90 --fail-under-functions 90 --json --summary-only \
  --output-path .state/rust-filelock-coverage.json
```

Earlier NUL errno mismatch, stale Go helper, clippy and coverage failures are
corrected; diagnostic logs are retained. The final recording follows all Go
reference edits. No app replay, shadow, full application check or release gate
is claimed by this unit.

## Plain Rust consumers and parent integration

`src/entry.rs` is the library entry. Default feature `bridge` wraps/re-exports
the generated bridge; `default-features = false` exposes only native `filelock`.
Serde, tempfile and scenario dispatch are optional bridge dependencies;
tempfile remains a test dependency. The generated bridge is preserved, so
refresh does not own the native module boundary.

After the plain build, `nm -D --defined-only target/debug/libfilelock_rs.so`
finds no `rust_anything_*` symbols. Evidence:
`.state/rust-filelock-plain-symbols.json`. This avoids the duplicate C symbols
verified by the parent when linking generated crates. The default bridge gate
continues to pass.

Parent next steps:

1. Add `filelock-rs = { path = "../filelock-rs", default-features = false }`,
   adjusting the relative path for the consumer. Use
   `filelock_rs::filelock::{LockFile, lock, try_lock, unlock, lock_path, Error}`.
2. Store retains PathLock through reload/save, explicitly releases it and reports
   ordered errors. Use `error_bytes()` for arbitrary-byte Go error output.
3. History retains the recorder lock and probes via a fresh LockFile open.
   Never probe through the holder/clone, infer liveness from a PID or unlink/
   replace a live lock pathname.
4. Wire consumers and run the owning store/history subprocess and app replay/
   shadow checks. No Go adapter enters the shipped runtime.
5. Obtain missing platform evidence below, reconcile the dedicated ticket and
   update MAP/HANDOFF. The generated gate alone does not complete migration.

## Remaining evidence and acceptance limits

| Gap | Evidence / attempted work | Next action |
| --- | --- | --- |
| Native macOS/Windows | Linux-only host, no native runner or Wine. Darwin arm64 and Windows amd64 checks pass; Windows one-byte/sharing tests prepared. | Run the default and no-default-feature cargo tests natively with Go and the uv-managed root Python environment available. Record OS/architecture/filesystem, native errno bytes, one-byte range, sharing, inheritance and exit cleanup. Missing native evidence blocks complete platform acceptance. |
| Six release targets | Installed relevant triples: Linux amd64, Darwin arm64, Windows amd64. No Linux arm64, Darwin amd64 or Windows arm64 build is claimed. | Parent's full six-target matrix compiles/links each target; check alone is not linking/native evidence. |
| Uncommon errors / filesystem variants | Common real errors and joined errors match; Unix adapts native errno casing. Unusual errno text, network mount semantics and Windows non-UTF-8 input conversion are not exhaustively qualified. | Compare encountered native errno bytes. If NFS/CIFS is in supported acceptance scope, run the process suite on the actual mount. Preserve errno distinctions. |
| Exceptional faults | Failure handling is implemented. Closed/O_PATH failures and repeated-release joins execute; no live-file close failure, ENOLCK exhaustion or injected EINTR is claimed. | Add OS-controlled fault evidence through the owning integration workflow; these remain uncovered evidence, never simulated success. |
| Application integration | Plain library has no bridge C symbols. Native hook output diagnostics consume its OS error rendering. Store/history integration remains open. | Parent wires store/history and runs their full behavioral acceptance; no Go child remains in the shipped path. |

No source API is an expected-result stub. Native macOS/Windows and app
integration are material external acceptance dependencies, not completed work.

## Exact files and cleanup

Reference:

- `rust/reference/filelock/filelock.go`
- `rust/reference/filelock/filelock_test.go`
- `rust/reference/filelock-inputs.jsonl`
- `rust/reference/filelock-native.py`

Rust:

- `rust/filelock-rs/.gitignore`
- `rust/filelock-rs/Cargo.toml`
- `rust/filelock-rs/Cargo.lock`
- `rust/filelock-rs/README.md`
- `rust/filelock-rs/rust-anything.json`
- `rust/filelock-rs/recorded-from.json`
- `rust/filelock-rs/cases.jsonl`
- `rust/filelock-rs/src/entry.rs`
- `rust/filelock-rs/src/filelock.rs`
- `rust/filelock-rs/src/lib.rs`
- `rust/filelock-rs/src/port.rs`
- `rust/filelock-rs/src/wire.rs`
- `rust/filelock-rs/tests/contracts.rs`
- `rust/filelock-rs/tests/native.rs`
- `rust/filelock-rs/tests/support/probe.rs`
- `rust/filelock-rs/go/go.mod`
- `rust/filelock-rs/go/filelockreference.go`

This ledger and `.state/rust-filelock-*` hold reports, timings, diagnostic logs
and runner/cleanup helpers. The parent's pre-existing
`.state/rust-filelock-ticket.json` is preserved untouched. Generated ignored
`.rust-anything` drivers and normal target state are development artifacts.

Final native cleanup command:
`cargo llvm-cov clean --manifest-path rust/filelock-rs/Cargo.toml --locked`.
Before removal, workspace/target/build paths, no target overrides, no symlink,
no active helper process and the retained exported report are verified. It
removes 131,166,208 allocated bytes of completed coverage artifacts in 0.100s.
Crate allocation falls from 359,006,208 to 227,840,000 bytes. Normal incremental
state, release bridge and reports survive; artifacts remain below 1GB.
`.state/rust-filelock-cleanup.json` records zero remaining unit processes and
zero owned temporary fixtures. No shared cache or other unit artifact is
removed. Scoped `git diff --check` passes; the explicit whitespace scan of all
22 owned source/reference/ledger files also passes, including untracked files
that Git's diff check does not inspect.
