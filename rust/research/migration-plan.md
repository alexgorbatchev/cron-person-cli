---
created_on: 2026-11-25 07:25
last_modified: 2026-11-25 07:25
status: current
---

# Rust migration proposal

This is the proposed plan, not approval to start a port. The user requested the complete rewrite using rust-anything. The skill requires approval of this plan and its first unit. The decision is pending on the existing Rust rewrite Agent Page. Accepted decisions, unit claims and measured migration state will be recorded in `rust/DECISIONS.md` and `rust/MAP.md` after that checkpoint.

## Verified baseline

The reference is Go revision `5fa7005813c99bc54ebef41447d4adb2aaa6c32a`. The app is a CLI and an optional local HTTP server. It installs system-crontab entries; it is not an in-process cron scheduler. Its existing React CRUD dashboard uses `@alexgorbatchev/shadcn` and is embedded in the Go executable.

The [analyzer report](analyze.json) identifies 12 Go units: six ready and six caution, with no blocked units or parse errors. Its `ui_files: 0` result misses the frontend. Its CLI-only shape also misses dashboard HTTP compatibility. The source-backed [runtime dependency report](runtime-dependencies.md) and [platform report](platform-semantics.md) supplement those omissions. Their candidate APIs and versions have not passed executable parity tests.

`just test` and `just vet` passed against the unmodified application during planning. A separate Go reference executable was built locally for characterization; it is ignored and will not be published as source. The [CLI baseline](cli-baseline.json) records read-only outputs twice per case. It is an initial characterization supplement, not the full rust-anything recording or a completed migration gate.

## Proposed outcome and equivalence bar

- A standalone Rust `cron-person` executable, with the existing React assets embedded in both debug and release builds. No Go, Bun or Node process is required at runtime.
- Preserve every current command, argument, flag, default, environment variable, error/exit behavior, shell hook, generated invocation and agent output. Preserve the hierarchical help tree, width rules, raw version output and generated skill installation output.
- Preserve `.cronrc`, configuration, authorization and history formats. No state migration or silent reapproval occurs. Authorization hashes original file bytes and execution uses the inspected snapshot.
- Preserve dashboard routes, body limits, method/error behavior, loopback binding, token and Origin checks, revision conflicts, static assets and security headers.
- Match stable output exactly. Only demonstrated timestamps, random tokens and invocation paths are candidates for narrowly specified normalization. Line/order noise and any intentional difference require a decision before acceptance.
- Preserve Linux, macOS and Windows amd64/arm64 release assets and embedded version contracts. Native process/locking checks supplement cross-compilation; a successful build alone does not prove platform parity.
- Retain Go source as reference. Replace the default build only after all gates pass. Do not publish a new tagged release or alter an installed development override as part of this request.

## Candidate dependencies and missing fits

Use clap for parsing, Serde for JSON, Axum/Tokio with configurable Hyper serving for HTTP, an always-embedded asset mechanism, and standard/platform filesystem and process APIs. Resolve and lock actual dependencies before claiming a minimum supported Rust version. The available Rust 1.100 toolchain is not by itself the supported-version decision.

There is no established exact Rust replacement for cobra-help-tree or lnquy cron descriptions. Custom compatibility rendering and cron adaptation are explicit parts of this proposal, subject to differential checks, licensing review and the skill's code-size gate. Evaluate `croner` and `cron-descriptor` against the Go corpus before selecting them. Do not add a second scheduler or accept a broader cron grammar. If the adapter becomes an independent parser or cannot match the corpus, stop that unit and present the remaining tradeoff.

YAML is used only by the Go release verifier, so it does not belong in the Rust runtime. Rust Testcontainers is a candidate for the opt-in real-daemon harness; per-exec user selection and live binary replacement require an adapter. Preserve the fixture's upgrade test rather than replacing it with a container-start check. Detailed candidates, exact source-backed incompatibilities and required checks are in the two research reports.

## Execution order

The analyzer suggests display, crontab, hook and config as starting candidates. Display is the smallest independent first unit; moving crontab before its storage dependencies would add avoidable glue. Keep the dependency waves and separate final packaging work:

| Order | Unit | First evidence required | Completion evidence |
| --- | --- | --- | --- |
| Baseline | Whole CLI and dashboard | Isolated HOME/XDG/crontab and task fixtures; existing tests, examples and failure paths | Original recorded twice; reviewed noise; original replay 100%; before screenshots |
| Entry point | `rust/app` | Approved decisions and full baseline | Temporary Go fallback faithfully passes the whole-app replay |
| First unit | `internal/display/` | Home lookup/cleaning, separators, punctuation and byte-boundary behavior | Bridge, recorded cases, Rust port, diff/bench/gate; replay remains 100% |
| Foundation wave | `internal/cronrc/`, `internal/filelock/` | Runtime and platform research; raw hashes; Go cron masks/text/errors; cross-process locks | Per-unit gates; no broadened grammar or PID-based liveness |
| State wave | `internal/history/`, `internal/store/` | Stable lock files, commit phases, history error independence, output bytes and liveness races | Per-unit gates; fault-injection and mixed-process evidence; replay 100% |
| Runtime wave | `internal/config/`, `internal/crontab/`, `internal/hook/`, `internal/runner/` | State dependencies; generated outer shell; config defaults; Unix/Windows lifecycle | Per-unit gates; subprocess, group termination and recorder detachment checks |
| Application wave | `cmd/cron-person/`, including dashboard | All internal units; exact help/parser/output and HTTP fixtures | Every command/route uses Rust; bundled frontend verified; fallback rate zero |
| Tooling wave | `scripts/dev-bootstrap.go`, `scripts/verify-release/` and build/release integration | Version/dev override and archive/checksum contracts; daemon fixture adapters | Rust recipes and six release targets; no Go build/runtime dependency for delivered app/tooling |
| Lift and cutover | All crates and default entry point | Every unit gated, full replay 100%, fallback zero | Successful `app lift`; repeated pure-binary replay; coverage, capacity, native targets and UI acceptance |

For each unit: claim its map entry; read its risks; run bridge and record against Go; write `src/port.rs`; build, diff, benchmark and gate; wire sequentially; replay the app. Keep the temporary kill switches until lift. Do not edit recordings to conceal a mismatch. Maintain per-unit task ownership and measured results in the map.

The requested checkpoint policy is approval through completion: report successful waves and continue, pausing only for a material behavior/architecture change, failed required gate or externally blocked validation. The alternative on the decision page is approval after each wave. Neither policy permits an unvalidated cutover.

## Acceptance and remaining evidence

Every port requires rust-anything gate success, fmt, Clippy with warnings denied and meaningful tests. Preserve the Go per-package 90% statement gate while Go remains active; apply the programming baseline's Rust coverage requirements to the Rust implementation. The final binary passes the complete CLI/HTTP corpus at 100% with zero fallbacks, and its own test command runs those cases without Go.

Translate existing hermetic tests and retain the real Debian cron test as an opt-in integration check. Add source-identified boundary cases where existing assertions prove only fragments: complete cron descriptions, help/agent snapshots, HTTP method/default differences, stable lock identity, Windows sharing and replacement, Darwin sync fallback, output drain and detached recording. A source-documented candidate is not a passed check.

Measure existing 10/100/1000-directory capacity workloads before and after whole-store changes. Capture and inspect comparable help/dashboard screenshots, obtain blind review, and present the before/after evidence. Build or check all six targets and run native Linux/macOS/Windows tests for OS-specific contracts. Record unavailable native evidence explicitly and keep dependent acceptance open.

The immediate next step is the user's submitted plan/first-unit choice. Preparation and research can continue; no unit has been bridged or ported, and there is no Rust application executable yet.
