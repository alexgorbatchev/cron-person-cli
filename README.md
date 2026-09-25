`cron-person` is a higher-order crontab manager for developers and operators that executes cron tasks in their respective directory contexts. It combines per-directory `.cronrc` schedules with direnv-style allow/deny authorization and shell integration to prevent unauthorized task execution.

# What It Does

- **Per-Directory Cron Schedules**: Defines scheduled tasks alongside project code inside localized `.cronrc` files.
- **Directory Working Context**: Executes every task with its working directory set to the folder containing `.cronrc`.
- **Environment Injection**: Loads custom environment variables declared inside `.cronrc` into the task execution environment.
- **Direnv-Style Authorization**: Protects systems by requiring explicit approval (`cron-person allow`) before any directory schedule can execute.
- **Tamper Detection**: Automatically invalidates authorization when `.cronrc` content changes, blocking unsanctioned modifications.
- **Shell Integration**: Alerts developers when entering directories with unauthorized or modified `.cronrc` files in Bash, Zsh, or Fish.
- **Non-Destructive System Sync**: Merges managed directory tasks into the user's system crontab inside an isolated, tagged block without modifying existing personal cron jobs.
- **Dual-Mode Output**: Switches between human terminal output and token-conservative machine output when `AGENT=1` is set.

# How It Works

- Place a `.cronrc` file formatted like a standard crontab in any project directory.
- Run `cron-person allow` to approve the directory and record its SHA-256 content checksum.
- Sync active directory jobs into your user crontab with `cron-person crontab sync`.
- When cron fires, `cron-person exec` sets the current working directory to the project folder, applies `.cronrc` environment variables, validates the authorization hash, and executes the scheduled command.
- Add the shell hook to your profile so your prompt alerts you whenever an unapproved or modified `.cronrc` is encountered.

# How it Really Works

- **State Persistence**: Records authorized directory paths, `.cronrc` locations, SHA-256 hashes, and approval timestamps in `$XDG_STATE_HOME/cron-person/allowed.json` (fallback `~/.local/state/cron-person/allowed.json`).
- **Security Check Before Run**: Before executing any command via `cron-person exec`, calculates the current SHA-256 hash of `.cronrc` and compares it against the state store. If the directory is unauthorized or the hash differs, execution aborts with exit code `1`.
- **System Crontab Splicing**: Reads user crontab via `crontab -l`, isolates the section between `# >>> cron-person managed block (DO NOT EDIT DIRECTLY) >>>` and `# <<< cron-person managed block <<<`, replaces only that section with compiled entries, and updates the crontab via `crontab -`.
- **Shell Integration**: Evaluates the nearest ancestor `.cronrc` upon directory change; writes a warning to stderr when an unapproved file is found while keeping shell exit statuses unaffected.
- **Agent Mode (`AGENT=1`)**: When `AGENT=1` is present in the environment, output disables decorative tree glyphs and tables, printing compact key-value lines for AI agents and automated scripts.

# Prerequisites

- [cron](https://man7.org/linux/man-pages/man5/crontab.5.html) daemon with standard `crontab` CLI support.

# Installation

Download the prebuilt binary for your platform from the [latest release](https://github.com/alexgorbatchev/cron-person-cli/releases/latest).

```bash
# macOS (Apple Silicon)
curl -sSL https://github.com/alexgorbatchev/cron-person-cli/releases/latest/download/cron-person_0.1.0_darwin_arm64.tar.gz | tar -xz -C ~/.local/bin
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
| `list` | | List all scheduled tasks across all authorized directories |
| `run` | `<dir> <command...>` | Manually run task command inside directory context |

### `cron-person exec`

| Command | Arguments | Description |
| :--- | :--- | :--- |
| `exec` | `<dir> [--] <command...>` | Internal execution engine invoked by crontab runner |

# License

[MIT](LICENSE) © Alex Gorbatchev
