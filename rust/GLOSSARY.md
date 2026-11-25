---
created_on: 2026-11-25 07:38
last_modified: 2026-11-25 07:38
status: current
---

# Migration domain terms

Shared names for implementers. Add concrete Rust module/type names as each unit is claimed.

| Original term | Meaning | Rust name |
| --- | --- | --- |
| .cronrc | Per-directory task definitions and environment assignments | cronrc module; CronFile and Task |
| authorization store | JSON records of approved raw .cronrc hashes | store module; Store and Record |
| snapshot | Inspected file bytes and identity used for an execution decision | cronrc/store snapshot value |
| schedulable | Approved, present and unchanged task definitions | store schedulable result |
| run history | Metadata, stream output and lock-backed execution liveness | history module; Run and Recording |
| task ID | Stable identity of a directory/command pair | history task_id |
| run ID | Unique recording identity shown by history/log commands | history run_id |
| managed block | Delimited system-crontab entries owned by cron-person | crontab module |
| prompt hook | bash/zsh/fish integration warning about authorization | hook module |
| display path | Home-folded diagnostic text, never an execution path | display path/error functions |
| dashboard document | .cronrc bytes, current revision and authorization status | dashboard Document |
| reference adapter | Test-only data boundary invoking the real implementation | rust/reference Go helpers |
| fallback | Temporary delegation to original Go executable | front-door state; removed at lift |
