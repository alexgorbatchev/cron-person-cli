---
created_on: 2026-11-25 07:38
last_modified: 2026-11-25 08:08
status: current
---

# Migration resume point

Read [MAP](MAP.md) for claims, [DECISIONS](DECISIONS.md) for accepted behavior and continuation policy, and [the proposal](research/migration-plan.md) for validation boundaries. Load the requested rust-anything skill and its whole-app workflow.

Current checkpoint: approval through completion received. The [application baseline](units/application-baseline.md) records 556 CLI and 337 HTTP cases with 100% original and Rust-entry replay. All application behavior still falls back to Go. The Rust binary is `rust/app/target/debug/cron-person`; its fallback already supplies the CLI fixture adapter.

The [display ledger](units/display.md) covers both real functions, 1,975 passing cases and a passing generated gate; application wiring and native platform validation remain open. Cronrc and filelock delegates own only their reference/unit/ledger paths in this shared checkout; the parent owns integration, MAP/HANDOFF and application code. Do not overwrite their active work. The parent continues baseline/display integration and independent application work while they port.

The tracker owns integration and session records. GitHub GraphQL mutations currently fail with internal errors; REST reads/comments work, while issue creation is intermittent. Preserve partial success and local ledgers; reconcile claims/board state when available without inventing ownership. Reports live under `.state/rust-*`; pre-change screenshots and their metadata are retained under `.state/rust-ui-baseline/`. Omnigraph's Executor is unavailable in this tool context.
