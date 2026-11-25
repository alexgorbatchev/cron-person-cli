---
created_on: 2026-11-25 07:38
last_modified: 2026-11-25 07:38
status: current
---

# Accepted migration decisions

These decisions govern the complete Rust rewrite. The user approved the corrected [proposal](research/migration-plan.md) and first unit on 2026-11-25, with continuation through validated completion.

## ADR-001: Standalone Rust runtime and retained dashboard
Status: accepted (2026-11-25)
Context: The existing application includes a CLI, system-crontab integration and an embedded React CRUD dashboard.
Decision: Port all Go application and build-helper behavior to Rust. Keep the existing @alexgorbatchev/shadcn frontend and embed its assets in the executable. Keep Go source as reference.
Consequences: The delivered runtime needs no Go, Bun or Node process. A second scheduler and frontend rewrite are outside scope.

## ADR-002: Preserve observable contracts
Status: accepted (2026-11-25)
Context: Commands, shell integration, existing state and dashboard clients depend on current behavior.
Decision: Preserve CLI arguments/flags/defaults/env, errors and exit codes, help tree and width behavior, generated commands, raw authorization hashes, config/history/store formats, cron grammar/descriptions and HTTP/security contracts. Match stable bytes exactly.
Consequences: Normalize only demonstrated timestamps, random tokens and invocation paths in characterization. Any other intentional difference requires a user decision. No silent state migration or reapproval.

## ADR-003: Rust dependency direction
Status: accepted (2026-11-25)
Context: There are Rust candidates for parsing, HTTP and serialization, but no proven exact help/cron-description replacement.
Decision: Use clap, Axum/Tokio, Serde and suitable native platform primitives. Validate croner/cron-descriptor candidates before selecting them; preserve help rendering and cron semantics with explicit tested adaptation where needed.
Consequences: Lock actual versions and verify unfamiliar APIs. Do not claim parity or an MSRV before executable evidence. Escalate an adapter that becomes an independent parser or cannot match the corpus.

## ADR-004: Genuine gates and explicit omitted behavior
Status: accepted (2026-11-25)
Context: rust-anything 4.1.0 cannot bridge several original APIs, and a partial gate omits behavior.
Decision: Use test-only data wrappers around real Go APIs and real Rust implementations, plus native/subprocess/app checks. Inventory source methods, excluded exports and side effects in each unit ledger. The first unit includes both Path and Error formatting.
Consequences: A gate covers only its recorded boundary. No omitted behavior counts as done. Prove adapter feasibility; stop a unit if genuine gating cannot be achieved. No Go adapter ships in the final binary.

## ADR-005: Continue through validated waves
Status: accepted (2026-11-25)
Context: The user requested completion and selected approval through completion.
Decision: Continue automatically through passing units and waves, including routine task-owned commits and pushes. Pause only for material contract/architecture choices, failed mandatory gates or external validation blockers.
Consequences: Routine unit/wave approvals are waived. This does not waive tests or accept behavior differences.

## ADR-006: Cutover and platform scope
Status: accepted (2026-11-25)
Context: Existing releases target Linux/macOS/Windows on amd64/arm64.
Decision: Preserve all six release targets and their version/archive contracts. Switch the default build to Rust only after genuine unit gates, complete CLI/HTTP replay at 100%, zero Go fallbacks, coverage, capacity, daemon, native-platform and UI evidence pass.
Consequences: Native tests supplement compilation. Missing evidence keeps dependent acceptance open. Do not publish a new tagged release or alter the installed development override.
