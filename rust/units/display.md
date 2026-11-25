---
created_on: 2026-11-25 08:08
last_modified: 2026-11-25 08:38
status: current
---

# Display parity ledger

Both original exports have actual Rust implementations in `display-rs/src/port.rs`. The unit passes its genuine rust-anything gate on Linux; application wiring and native platform execution remain open.

| Source behavior | Bridge boundary | Additional evidence |
| --- | --- | --- |
| `display.Path(string)` | `FormatPath`, `PathBytes` | Exact-home/descendant/sibling, lexical cleaning, root, absent/empty HOME, arbitrary bytes |
| `display.Error(error)` | `FormatErrorText`, `ErrorBytes` call the real function using `errors.New` | Repeated occurrences, adjacent byte delimiters, Unicode encoded as adjacent bytes, invalid UTF-8 |
| OS-selected HOME/USERPROFILE | Explicit-home byte adapters set/restore the real Go environment | Go subprocess verifies absent-home behavior; native Windows test requires Windows execution |
| Nil error | Not representable by the text adapter | Go native test pins the original panic; Rust diagnostic entry receives already-rendered error bytes, never nullable errors |
| Windows drive/UNC/device and relative cleaning | Platform-selected implementation | Source-derived Windows lexical cases execute on Linux; Windows-specific formatting test is checked but not yet run natively |

The bridge wraps real production Go functions, not reproduced results. Base64 preserves invalid strings across JSON/FFI. The Rust byte API retains arbitrary diagnostic bytes; string adapters are only the generated bridge's valid-UTF-8 boundary. HOME prefixes are case-sensitive and lexical, matching the original; no filesystem resolution occurs. The diagnostic delimiter checks deliberately examine individual bytes rather than decoded Unicode scalar values.

## Checks

`rust-anything record rust/display-rs --inputs rust/reference/display-inputs.jsonl` records 1,975 cases, including 501 errors: 225 FormatPath, 225 FormatErrorText, 762 PathBytes and 763 ErrorBytes cases. `build`, `diff` and `gate` pass; diff is 1,975/1,975. Gate covers fmt, warning-free Clippy, Rust tests, safety annotations, lockfile, nonstub/size/error/panic checks, actual build/API and behavior. Retained reports are `.state/rust-display-{diff,gate,bench}.json`.

Go reference tests pass with 96.3% statement coverage. Rust native tests cover byte/boundary/lexical/error behavior. First-party Rust coverage uses:

```sh
cargo llvm-cov --manifest-path rust/display-rs/Cargo.toml --ignore-filename-regex '/src/(lib|wire)\.rs$' --summary-only --fail-under-lines 90 --fail-under-functions 90
```

The only excluded source files are unmodified rust-anything-generated FFI/JSON transport. All first-party formatter, lexical path implementation and byte adapters are included: 98.69% lines, 95.12% functions. The source-derived path code and tests retain Go's BSD license in `display-rs/THIRD_PARTY_NOTICES.txt`, which final distribution packaging must include. The crate declares Rust 1.100, the compiler used for these checks.

`cargo check --locked` passes for installed `x86_64-pc-windows-gnu` (1.444 s) and `aarch64-apple-darwin` (1.669 s). This does not establish native environment/encoding behavior. Other release-target checks and native Go/Rust platform comparisons remain open.

The recorded bridge benchmark is slower than the compiled Go formatter: FormatPath 43.528→1,083.071 ns, FormatErrorText 75.145→1,361.737 ns, PathBytes 230.590→1,934.275 ns, ErrorBytes 266.476→819.931 ns. These measurements include FFI/JSON overhead and include the expanded Windows cleaner. They do not establish standalone application performance. Measure actual callers before cutover.

## Remaining acceptance

The library entry point is `src/entry.rs`. Its default `bridge` feature retains
the generated C exports and JSON dispatcher. Native Rust dependents select
`default-features = false` to expose `port` without those exports; this fixes
the duplicate C symbols encountered when linking multiple generated units.
The lexical `clean_path` byte API is public for configuration path reuse.
The actual native configuration and filelock libraries link and execute in
one consumer alongside the default display bridge. The renewed default gate
passes, and first-party coverage remains 98.69% lines / 95.12% functions.
Ignored `.state/rust-display-feature-*` and `.state/rust-native-consumer.log`
retain the reports. This is native library compatibility, not application
command wiring.

Wire real application callers after their dependent commands are ported; preserve the temporary original switch until lift. Do not label a no-op dispatch as wiring. Run native macOS/Windows comparisons, including HOME/USERPROFILE encoding. Test the selected final MSRV, carry the license into packaged artifacts, and rerun final coverage/bench and whole-app replay. The generated gate covers the reference adapter boundary only.
