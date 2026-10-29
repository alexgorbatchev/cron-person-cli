---
name: cron-person
description: >-
  Use when operating the cron-person CLI: authorizing per-directory .cronrc
  schedules, syncing them into the user crontab, running tasks manually, or
  inspecting task run history and captured logs.
author: alexgorbatchev
metadata:
  created_on: 2026-10-29 11:31
  last_modified: 2026-10-29 12:18
  status: current
---

# cron-person

`cron-person` runs cron tasks declared in per-directory `.cronrc` files. A directory's tasks run only after its `.cronrc` is authorized with `cron-person allow`; any change to the file revokes that authorization until it is approved again. Authorized tasks are compiled into a tagged block of the user crontab, and every run is recorded with its exit status and captured output.

Set `AGENT=1` for compact `key: value` output. Every path printed under the home directory is shown as `~`.

## `.cronrc` format

A `.cronrc` uses crontab syntax: `NAME=value` lines define environment variables for every task, and schedule lines (`*/5 * * * * ./script.sh`, `@hourly make sync`) define tasks. Each task runs with its working directory set to the folder containing `.cronrc`, through `$SHELL -c` (the `.cronrc` `SHELL` wins, then the inherited `SHELL`, then `/bin/sh`). Tasks also receive `CRON_PERSON_DIR` (the directory) and `CRON_PERSON_CRONRC` (the `.cronrc` path).

## Workflows

Authorize a project and schedule its tasks:

```sh
cron-person dir check ~/projects/app   # validate syntax and list tasks
cron-person allow ~/projects/app       # record the .cronrc SHA-256 hash
cron-person crontab sync               # install authorized tasks into the user crontab
```

Investigate a failing task:

```sh
AGENT=1 cron-person task history ~/projects/app --limit 5
AGENT=1 cron-person task log <run-id> --tail 50
```

Run a task by hand and record it as a manual run:

```sh
cron-person task run ~/projects/app ./scripts/backup.sh
```

Watch a run that cron, or a detached `task run`, started:

```sh
cron-person task log --follow <run-id>
```

## Global flags

These apply to every command.

- `--store` (string, default empty): authorization store file. Empty selects `$XDG_STATE_HOME/cron-person/allowed.json`, or `~/.local/state/cron-person/allowed.json` when `XDG_STATE_HOME` is unset or relative. Run history lives in `runs/` beside the store.
- `--config` (string, default empty): configuration file. Empty selects `$XDG_CONFIG_HOME/cron-person/config.json`, or `~/.config/cron-person/config.json`.
- `--help`, `-h` (bool): print help for the command and its subcommands.
- `--version`, `-v` (bool): print the bare version string.

## Environment

- `AGENT`: `1`, `true`, or `yes` switches all output, errors, and help to compact agent mode.
- `XDG_STATE_HOME`: base directory for the default store and run history when absolute.
- `XDG_CONFIG_HOME`: base directory for the default configuration file when absolute.
- `SHELL`: shell used to run tasks when the `.cronrc` does not set `SHELL`.

## Configuration

`config.json` is optional; omitted settings keep their defaults and unknown keys are rejected.

```json
{
  "history": {
    "retention_days": 30,
    "max_size_mb": 100,
    "max_run_mb": 10
  }
}
```

- `history.retention_days` (default `30`): finished runs older than this are deleted. `0` disables the age limit.
- `history.max_size_mb` (default `100`): oldest finished runs are deleted until all history fits. `0` disables the size limit.
- `history.max_run_mb` (default `10`): output a single run produces beyond this is not stored; the log ends with `[cron-person] output truncated after N bytes` and the run is marked truncated. The task's own output streams are unaffected. `0` stores everything.

Retention is enforced after every recorded run. Runs in progress and the newest run of each task are never deleted, so one large run can exceed the size limit.

An invalid configuration never stops or fails a task. The run uses the default settings and records the problem as a warning, shown by `task history` and `task log`; a manual run also prints it to stderr. `cron-person status` reports the configuration as invalid, and `cron-person crontab sync` refuses to install schedules until it is fixed.

## Commands

### `cron-person allow [path]` / `cron-person dir allow [path]`

Authorizes the `.cronrc` in `path`, recording its SHA-256 hash. Without `path`, uses the nearest `.cronrc` in the current directory or its ancestors, falling back to the current directory. Agent output: `dir`, `status: allowed`, `hash`. Writes the store.

### `cron-person deny [path]` / `cron-person dir deny [path]`

Revokes authorization for the directory, resolved like `allow`. Its tasks stop running and are dropped from the crontab on the next sync. Agent output: `dir`, `status: denied`. Writes the store.

### `cron-person dir list`

Lists tracked directories with their status: `allowed`, `changed` (the `.cronrc` no longer matches its approved hash), `unauthorized`, `missing_cronrc`, or `orphan` (the directory is gone). Agent output: one tab-separated line per directory with `dir`, `status`, and `hash`.

### `cron-person dir check [path]`

Parses the `.cronrc` in `path` (resolved like `allow`) and reports its tasks and environment variables without changing anything. Agent output: `path`, `tasks_count`, `hash`.

### `cron-person dir status [path]`

Shows authorization status, recorded hash, approval time, and tasks for one directory (resolved like `allow`). Agent output: `dir`, `status`, and `hash` when tracked.

### `cron-person status`

Summarizes the installation and the configuration file's state: `defaults` (no file), `loaded`, or `invalid` with the reason. Agent output: `version`, `tracked_dirs`, `allowed_dirs`, `total_tasks`, `config`, `config_status`, and `config_error` when invalid.

### `cron-person crontab sync`

Compiles every authorized directory's tasks and replaces the managed block of the user crontab via `crontab -l` and `crontab -`, leaving other entries untouched. Each generated entry calls this binary with the absolute `--store` and `--config` paths, so rerun `crontab sync` after moving the binary or changing those paths. Fails without changing the crontab when the configuration file is invalid. Agent output: `synced_directories`, `status: success`.

### `cron-person crontab show`

Prints the managed block `crontab sync` would install, without modifying the crontab.

### `cron-person crontab uninstall`

Removes the managed block from the user crontab. Agent output: `crontab: uninstalled`.

### `cron-person task list`

Lists tasks of authorized directories with readable schedules and their `.cronrc` source lines. Agent output: one tab-separated line per task with `dir`, `file`, `line`, `schedule`, `description`, and `cmd`.

### `cron-person task run <dir> <command...>`

Runs `command` in `dir` exactly as cron would, after the same authorization check, and records it with trigger `manual`. Output streams to the terminal and into the run log; typed input is forwarded to the task line by line. The exit code is the task's own exit code, `128+N` when the task was killed by signal `N`, or `1` for any other failure.

The run is recorded by a separate background process, so it can outlive the terminal session:

- When stdin is a terminal, Ctrl+C asks `Also terminate the task? [y/N]`. `y`, or a second Ctrl+C, terminates the task and exits with its status. Any other answer detaches: the command exits `0` and prints the run ID (agent output on stderr: `detached: true`, `run_id`), while the task keeps running and its output keeps being recorded. A detached task reads end-of-input on stdin; follow it with `task log --follow <run-id>`.
- When stdin is not a terminal, Ctrl+C, SIGTERM, and SIGHUP terminate the task without asking.

Terminating sends SIGTERM to the task's whole process group and SIGKILL 10 seconds later if anything is still running.

### `cron-person task history [dir]`

Lists recorded runs newest first, from cron and `task run`, including refused runs. `dir` limits the list to tasks in that directory.

- `--task` (string, default empty): show only runs of this task ID.
- `--limit` (int, default 20): maximum number of runs; `0` lists all.

Agent output: one tab-separated line per run with `id`, `task`, `status`, `exit` (`-` when there is no exit status), `trigger` (`cron` or `manual`), `started` (RFC 3339, UTC), `duration_ms`, `warnings` (count), `dir`, and `cmd`. Statuses:

- `succeeded`, `failed`: the command finished with the recorded exit status.
- `blocked`: the run was refused before starting, for example because the `.cronrc` changed; `task log` shows the reason.
- `running`: the recording `cron-person` process is still alive.
- `interrupted`: the recording process ended without recording an outcome.

A task ID is derived from the directory and the exact command, so editing a command in `.cronrc` starts a separate history.

### `cron-person task log <run-id>`

Prints a run's metadata followed by its captured output. Each output line is prefixed `[out] ` or `[err] ` by stream; order is exact within a stream and approximate between streams, and lines longer than 64 KiB are split.

- `--tail` (int, default 0): print only the last N output lines; `0` prints all.
- `--follow`, `-f` (bool): after the existing output, keep printing output as it is captured until the run finishes, then print how it ended. Ctrl+C stops following without affecting the run.

Agent output: `id`, `task`, `status`, `exit`, `trigger`, `started`, `duration_ms`, `dir`, `cmd`, `error` when present, one `warning` per warning, `truncated: true` when output was cut at `history.max_run_mb`, then `output:` followed by the raw log lines. With `--follow`, the output is followed by `final_status`, `final_exit`, and `final_duration_ms`.

### `cron-person exec <dir> [--] <command...>`

The entry point generated crontab lines call. Runs and records the task with trigger `cron`, in the foreground with no prompt. SIGINT, SIGTERM, and SIGHUP terminate the task's process group as described under `task run`, and the outcome is still recorded. When the task exits but a process it left in the background keeps its output open, capture stops after 10 seconds and the run records a warning; redirect the output of background processes.

### `cron-person hook <bash|zsh|fish>`

Prints a shell hook to `eval` (Bash, Zsh) or `source` (Fish). On every prompt it warns on stderr when the nearest `.cronrc` is unauthorized or modified, and preserves the previous command's exit status.

### `cron-person skill`

Prints the Skills CLI command that installs this skill and the skill's URL, identically in both modes.

### `cron-person help [command]`

Prints help for a command, equivalent to `cron-person <command> --help`.

### `cron-person completion <bash|fish|powershell|zsh>`

Prints a shell completion script. Each shell subcommand (`cron-person completion bash`, `cron-person completion fish`, `cron-person completion powershell`, `cron-person completion zsh`) accepts `--no-descriptions` (bool) to omit command descriptions.

## Errors and exit codes

Errors go to stderr as `[ERROR] <message>`, or `ERR: <message>` in agent mode. Commands exit `0` on success and `1` on failure, except task runs, which exit with the task's own code as described under `task run`. A run whose history cannot be written still executes; the command then exits non-zero with the recording error.
