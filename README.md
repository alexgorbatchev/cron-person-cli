`cron-person` is a higher-order crontab manager for developers and operators that executes cron tasks in their respective directory contexts. It combines per-directory `.cronrc` schedules with direnv-style allow/deny authorization and shell integration to prevent unauthorized task execution.

# What It Does

- **Per-Directory Cron Schedules**: Defines scheduled tasks alongside project code inside localized `.cronrc` files.
- **Directory Working Context**: Executes every task with its working directory set to the folder containing `.cronrc`.
- **Environment Injection**: Loads custom environment variables declared inside `.cronrc` into the task execution environment.
- **Direnv-Style Authorization**: Protects systems by requiring explicit approval (`cron-person allow`) before any directory schedule can execute.
- **Tamper Detection**: Automatically invalidates authorization when `.cronrc` content changes, blocking unsanctioned modifications.
- **Shell Integration**: Alerts developers when entering directories with unauthorized or modified `.cronrc` files in Bash, Zsh, or Fish.
- **Non-Destructive System Sync**: Merges managed directory tasks into the user's system crontab inside an isolated, tagged block without modifying existing personal cron jobs.
- **Run History & Logs**: Records every scheduled and manual run, including runs refused by authorization checks, with exit status, timing, and captured output.
- **Dual-Mode Output**: Switches between human terminal output and token-conservative machine output when `AGENT=1` is set.

# How It Works

- Place a `.cronrc` file formatted like a standard crontab in any project directory.
- Run `cron-person allow` to approve the directory and record its SHA-256 content checksum.
- Sync active directory jobs into your user crontab with `cron-person crontab sync`.
- When cron fires, `cron-person exec` sets the current working directory to the project folder, applies `.cronrc` environment variables, validates the authorization hash, and executes the scheduled command.
- Run a task by hand with `cron-person task run`; press Ctrl+C to either stop it or leave it running in the background.
- Review recent runs with `cron-person task history`, read a run's captured output with `cron-person task log <run-id>`, and watch one in progress with `cron-person task log --follow <run-id>`.
- Add the shell hook to your profile so your prompt alerts you whenever an unapproved or modified `.cronrc` is encountered.

# How it Really Works

- **State Persistence**: Records authorized directory paths, `.cronrc` locations, SHA-256 hashes, and approval timestamps in `$XDG_STATE_HOME/cron-person/allowed.json` (fallback `~/.local/state/cron-person/allowed.json`).
- **Security Check Before Run**: Before executing any command via `cron-person exec`, calculates the current SHA-256 hash of `.cronrc` and compares it against the state store. If the directory is unauthorized or the hash differs, execution aborts with exit code `1`.
- **Exit Codes**: `cron-person exec` and `cron-person task run` exit with the task's own exit code, or `128+N` when the task is killed by signal `N`, so cron mail and wrapper scripts see the real result. Every other failure exits `1`.
- **System Crontab Splicing**: Reads user crontab via `crontab -l`, isolates the section between `# >>> cron-person managed block (DO NOT EDIT DIRECTLY) >>>` and `# <<< cron-person managed block <<<`, replaces only that section with compiled entries, and updates the crontab via `crontab -`.
- **Scheduled Execution**: Generated entries retain the absolute authorization store and configuration file paths, including custom `--store`, `--config`, and XDG locations. Shell operators, substitutions, and redirections execute inside the authorized project context. Percent signs in commands are preserved literally rather than interpreted as cron input separators. Run `cron-person crontab sync` after installing a new binary to regenerate existing entries.
- **Run History**: Each `cron-person exec` and `cron-person task run` invocation writes a run directory under `runs/` beside the authorization store (default `$XDG_STATE_HOME/cron-person/runs/`), holding `meta.json` and `output.log`. History directories are private to your user (`0700`, logs `0600`) because captured output can contain secrets.
- **History Retention**: After every recorded run, finished runs older than `history.retention_days` are deleted, then the oldest finished runs until all history fits within `history.max_size_mb` (defaults: 30 days, 100 MB; see [Configuration](#configuration)). Runs still in progress and the newest run of each task are always kept, so one large run can exceed the size limit.
- **Captured Output**: Task output still reaches the original stdout and stderr, so cron mail is unchanged, and every line is also written to `output.log` prefixed with `[out]` or `[err]`. Line order is exact within each stream; ordering between stdout and stderr follows arrival order and is approximate. Lines longer than 64 KiB are split across records, and output beyond `history.max_run_mb` (default 10 MB) is not stored, so a runaway task cannot fill the disk.
- **Background Processes**: Each task runs in its own process group (on Windows, a job object, so termination ends with Ctrl+Break and then the whole job). When the task exits but a process it started in the background keeps its output open, capture stops 10 seconds later and the run records a warning, so redirect the output of anything a task leaves running.
- **Manual Runs and Ctrl+C**: `cron-person task run` hands the run to a separate recording process and relays its input and output. In a terminal, Ctrl+C asks whether to terminate the task too: `y` (or a second Ctrl+C) sends SIGTERM to the task's process group, then SIGKILL after 10 seconds; anything else returns your prompt and leaves the task running and recorded in the background, with end-of-input on its stdin. Without a terminal, Ctrl+C terminates the task.
- **Task IDs**: A task ID is derived from the directory and the exact command, so editing a command in `.cronrc` starts a separate history for it.
- **Refused and Interrupted Runs**: Runs refused by authorization checks are recorded as `blocked` with the reason. A run is listed as `interrupted` when its `cron-person` process ended without recording an outcome; liveness comes from a lock the operating system releases when that process exits, so a reused process ID cannot make a dead run look alive. If history cannot be written, the task still runs and `cron-person` exits with an error after it finishes.
- **Invalid Configuration**: A configuration file that cannot be parsed never stops or fails a task. The run uses the default settings and records a warning, shown by `task history` and `task log`, so scheduled runs do not flood cron mail. Manual runs also print the warning, `cron-person status` reports it, and `cron-person crontab sync` refuses to install schedules until it is fixed.
- **Shell Integration**: Evaluates the nearest ancestor `.cronrc` upon directory change; writes a warning to stderr when an unapproved file is found while keeping shell exit statuses unaffected.
- **Agent Mode (`AGENT=1`)**: When `AGENT=1` is present in the environment, output disables decorative tree glyphs and tables, printing compact key-value lines for AI agents and automated scripts.

# Prerequisites

- [cron](https://man7.org/linux/man-pages/man5/crontab.5.html) daemon with standard `crontab` CLI support.

# Installation

Download the prebuilt binary for your platform from the [latest release](https://github.com/alexgorbatchev/cron-person-cli/releases/latest).

```bash
# macOS (Apple Silicon)
curl -fsSL https://github.com/alexgorbatchev/cron-person-cli/releases/latest/download/cron-person_0.0.1_darwin_arm64.tar.gz | tar -xz -C ~/.local/bin
```

# Quick Start

### 1. Create a `.cronrc` in your project

```bash
cat << 'EOF' > .cronrc
# Local project crontab
APP_ENV=production

# Run database backup every hour
0 * * * * ./scripts/backup.sh --daily

# Process message queue every 5 minutes
*/5 * * * * ./bin/worker-sync
EOF
```

### 2. Authorize the directory

```bash
cron-person allow
```

Sample Output:
```text
[OK] Authorized .cronrc in /home/alex/workspaces/my-app
     Hash: 3f8b0e8c8b4172f3e82d790dcfbcad4d440ad2f05df5e449a8fe100c86812891
```

### 3. Sync to system crontab

```bash
cron-person crontab sync
```

Sample Output:
```text
[OK] Synchronized 1 authorized directory task(s) into crontab
```

### 4. Enable shell integration (optional)

Add the hook to your shell configuration file:

```bash
# Bash (~/.bashrc)
eval "$(cron-person hook bash)"

# Zsh (~/.zshrc)
eval "$(cron-person hook zsh)"

# Fish (~/.config/fish/config.fish)
cron-person hook fish | source
```

# Options & Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--store <path>` | | `$XDG_STATE_HOME/cron-person/allowed.json` | Path to custom authorization state file |
| `--config <path>` | | `$XDG_CONFIG_HOME/cron-person/config.json` | Path to custom configuration file |
| `--help` | `-h` | `false` | Display command tree help |
| `--version` | `-v` | `false` | Display raw version string |

### `cron-person dir`

| Command | Arguments | Description |
| :--- | :--- | :--- |
| `allow` | `[path]` | Authorize `.cronrc` in the target directory |
| `deny` | `[path]` | Revoke authorization for target directory |
| `list` | | List all tracked directories and authorization status |
| `check` | `[path]` | Validate `.cronrc` syntax and display detected tasks |
| `status` | `[path]` | Show authorization status and tasks for specific directory |

### `cron-person crontab`

| Command | Arguments | Description |
| :--- | :--- | :--- |
| `sync` | | Compile authorized directory jobs and install into user crontab |
| `show` | | Preview generated crontab block to stdout without modifying crontab |
| `uninstall` | | Remove managed block from system crontab |

### `cron-person task`

| Command | Arguments | Description |
| :--- | :--- | :--- |
| `list` | | List tasks across authorized directories with readable schedules, original expressions, commands, and `.cronrc` file paths and line numbers |
| `run` | `<dir> <command...>` | Manually run task command inside directory context |
| `history` | `[dir]` | List recent runs, newest first, optionally limited to one directory |
| `log` | `<run-id>` | Show a run's status, timing, warnings, and captured output, optionally following it live |

`cron-person task list` groups tasks by directory. Each task includes its source as `path/to/.cronrc:line`, where the line number counts every line in the file, including comments and blank lines. Schedule descriptions use English and 24-hour time; the original cron expression is retained beside each description. This lists authorized configuration rather than confirming what is installed in the system crontab. Store, authorization inspection, and parsing errors fail the command without printing a partial task list.

```text
Total scheduled tasks: 1

Directory: ~/projects/example
  Every 30 minutes [*/30 * * * *]
    Source: ~/projects/example/.cronrc:2
    Command: ./sync.sh
```

With `AGENT=1`, each task is one tab-separated line containing `dir`, `file`, `line`, `schedule`, `description`, and `cmd` fields.

`cron-person task history` lists runs from both cron and `task run`:

```text
Task Runs:
  [ERROR] 20261029T174412.078633Z-44e3f844  failed (exit 3)
     Started: 2026-10-29 10:44:12 PDT  Duration: 1.204s  Trigger: cron
     Task: 5bacafc67753  ~/projects/example  ./sync.sh
```

Statuses are `succeeded`, `failed`, `blocked`, `running`, and `interrupted`. With `AGENT=1`, each run is one tab-separated line containing `id`, `task`, `status`, `exit`, `trigger`, `started`, `duration_ms`, `warnings`, `dir`, and `cmd` fields; `exit` is `-` when the command produced no exit status.

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--task <id>` | | | Show only runs of this task ID |
| `--limit <n>` | | `20` | Maximum number of runs to show; `0` shows all |

`cron-person task log <run-id>` prints the run's metadata followed by its captured output. With `AGENT=1`, metadata is printed as `key: value` lines, followed by `output:` and the raw log lines.

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--tail <n>` | | `0` | Show only the last N output lines; `0` shows all |
| `--follow` | `-f` | `false` | Keep printing output as it is captured until the run finishes, then report how it ended; Ctrl+C stops following without affecting the run |

Descriptive output, warnings, and errors display paths under your home directory as `~`. Generated crontab entries, shell integration scripts, task commands, and subprocess output retain their original paths.

### `cron-person exec`

| Command | Arguments | Description |
| :--- | :--- | :--- |
| `exec` | `<dir> [--] <command...>` | Internal execution engine invoked by crontab runner |

### `cron-person skill`

| Command | Arguments | Description |
| :--- | :--- | :--- |
| `skill` | | Print the Skills CLI install command and URL for the `cron-person` agent skill |

# Configuration

`cron-person` reads optional settings from `$XDG_CONFIG_HOME/cron-person/config.json` (fallback `~/.config/cron-person/config.json`, override with `--config`). Omitted settings keep their defaults, and unknown keys are rejected so a misspelled setting fails loudly instead of being ignored.

```json
{
  "history": {
    "retention_days": 30,
    "max_size_mb": 100,
    "max_run_mb": 10
  }
}
```

| Setting | Default | Description |
| :--- | :--- | :--- |
| `history.retention_days` | `30` | Delete finished runs older than this many days; `0` disables the age limit |
| `history.max_size_mb` | `100` | Delete the oldest finished runs until all history fits in this many megabytes; `0` disables the size limit |
| `history.max_run_mb` | `10` | Store at most this many megabytes of output per run; `0` stores everything |

Run `cron-person crontab sync` after moving the configuration file so scheduled entries use the new path.

# Name Rationale

Originally conceived as `cron-man` (short for *cron manager*). However, "cron man" felt out of touch with the times, so it became `cron-person` instead.

# License

[MIT](LICENSE) © Alex Gorbatchev
