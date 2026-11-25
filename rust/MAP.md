---
created_on: 2026-11-25 07:38
last_modified: 2026-11-25 08:08
status: current
---

# Rust migration map

Goal: a standalone Rust cron-person preserving the CLI, persisted state, platform contracts and embedded CRUD dashboard. [Accepted decisions](DECISIONS.md), [proposal and validation boundaries](research/migration-plan.md), [domain terms](GLOSSARY.md), [resume point](HANDOFF.md).

| Order | Unit | State | Evidence / coverage ledger |
| --- | --- | --- | --- |
| 0 | Full CLI/HTTP baseline | recorded in this checkpoint | [Coverage and limits](units/application-baseline.md): original replay 556/556 CLI and 337/337 HTTP; 34 inspected pre-change screenshots |
| 1 | Rust front door | scaffolded in this checkpoint | CLI 556/556 with fallback rate 1.0; HTTP 337/337 entirely through Go fallback |
| 2 | display | implemented by parent 2026-11-25; tracker claim unconfirmed | [Ledger](units/display.md): both APIs implemented; genuine gate and 1,975/1,975 diff pass; native platform and application wiring pending |
| 3 | cronrc | assigned to cronrc delegate 2026-11-25 | Parsing, descriptors, descriptions, raw-file reads/hashes and platform open behavior; delegate owns only unit paths |
| 3 | filelock | assigned to filelock delegate 2026-11-25 | No direct bridgeable exports; real multi-process scenario adapter required; delegate owns only unit paths |
| 4 | history | open | Recording lifecycle/output, retention, locks, durable replacement and error independence |
| 4 | store | open | Authorization snapshots, cross-process transactions, sync/replace failure paths |
| 5 | config | open | Strict JSON/defaults/path resolution and limits |
| 5 | crontab | open | Generation/quoting, managed blocks and actual system command behavior |
| 5 | hook | open | Shell scripts and export output |
| 5 | runner | open | No direct bridgeable exports; real execution/history/process scenario adapter required |
| 6 | CLI/dashboard | open | All commands, help, attachment and HTTP/static/security contracts |
| 7 | Build/release helpers | open | Development override, published-release verification and real daemon harness |
| 8 | Lift/default build | open | Every unit gate and ledger complete; complete replay100%, fallback0; platform/UI/coverage/capacity evidence |

No implementation unit is complete. Per-unit ledgers must identify source methods/exports/side effects, wrapper mappings, recorded case IDs, native/subprocess/app tests and unresolved gaps before claiming the unit. Generated gate success alone never closes a source unit. Run `rust-anything map --write` at implementation checkpoints and reconcile its measured PORT_MAP with this plan. The current generated snapshot includes active delegate work and a temporary cronrc scaffold; its changed-recording warnings remain delegate-owned until their completed handoffs. It does not establish application wiring or completion of those units.

Out of scope: tagged release publication, installed development override changes, a frontend rewrite, and an in-process scheduler. No open product decision remains at the starting checkpoint; any newly verified tradeoff follows ADR-005.
