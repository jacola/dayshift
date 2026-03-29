# CLI Reference

## Global Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--verbose` | bool | `false` | Enable verbose output |
| `--version` | | | Print version and exit |

---

## `dayshift init`

Create a dayshift configuration file at `~/.config/dayshift/config.yaml`.

```
dayshift init [flags]
```

### Flags

| Flag | Short | Type | Default | Description |
|---|---|---|---|---|
| `--force` | `-f` | bool | `false` | Overwrite existing config without prompting |

### Examples

```bash
# Create a new config (prompts if one exists)
dayshift init

# Overwrite existing config
dayshift init --force
```

---

## `dayshift run`

Process pending issues. Scans configured repositories for issues with the trigger label and executes the next pipeline phase for each.

```
dayshift run [flags]
```

### Flags

| Flag | Short | Type | Default | Description |
|---|---|---|---|---|
| `--issue` | | int | `0` | Process a specific issue number |
| `--phase` | | string | | Run only a specific phase (`research`, `plan`, `implement`, `validate`) |
| `--repo` | | string | | Process issues from a specific repo (`owner/repo`) |
| `--provider` | | string | | Use a specific AI provider (`claude`, `codex`, `copilot`) |
| `--dry-run` | | bool | `false` | Show what would be processed without executing |
| `--yes` | `-y` | bool | `false` | Skip confirmation prompt |

### Examples

```bash
# Process all pending issues (interactive confirmation)
dayshift run

# Process all pending issues without confirmation
dayshift run --yes

# Preview what would happen
dayshift run --dry-run

# Process a single issue
dayshift run --issue 42 --repo owner/repo

# Force a specific phase
dayshift run --issue 42 --repo owner/repo --phase research

# Use a specific provider
dayshift run --provider claude --yes

# Combine flags for targeted execution
dayshift run --issue 42 --repo owner/repo --provider codex --yes
```

---

## `dayshift status`

Show issue processing status. Lists all tracked issues with their current phase, title, and age. Also shows the last 5 runs.

```
dayshift status [flags]
```

### Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--issue` | int | `0` | Show detailed status for a specific issue (phase timing, PR URL) |
| `--repo` | string | | Filter by repository (`owner/repo`) |

### Examples

```bash
# Show all tracked issues and recent runs
dayshift status

# Filter to a specific repo
dayshift status --repo owner/repo

# Show detail for a specific issue
dayshift status --issue 42 --repo owner/repo
```

---

## `dayshift doctor`

Check configuration and environment health. Verifies that all required tools are installed and the config is valid.

```
dayshift doctor
```

### Checks Performed

1. Config file exists and is valid YAML
2. `gh` CLI is installed and authenticated
3. `claude` CLI is available (+ version)
4. `codex` CLI is available (+ version)
5. `copilot` CLI is available (+ version)
6. SQLite database can be opened
7. Each configured project path exists locally

### Examples

```bash
# Run all health checks
dayshift doctor

# With verbose output for debugging
dayshift doctor --verbose
```

---

## `dayshift daemon`

Manage the background polling daemon. The daemon runs on the schedule defined in `config.yaml` and processes issues automatically.

### `dayshift daemon start`

Start the background daemon.

```
dayshift daemon start [flags]
```

| Flag | Short | Type | Default | Description |
|---|---|---|---|---|
| `--foreground` | `-f` | bool | `false` | Run in the foreground instead of backgrounding |

The daemon writes a PID file to `~/.local/share/dayshift/dayshift.pid`.

### `dayshift daemon stop`

Stop the background daemon. Sends SIGTERM, waits up to 10 seconds, then sends SIGKILL if the process is still alive.

```
dayshift daemon stop
```

### `dayshift daemon status`

Show whether the daemon is currently running.

```
dayshift daemon status
```

### Examples

```bash
# Start the daemon in the background
dayshift daemon start

# Start in the foreground (useful for debugging)
dayshift daemon start --foreground

# Check if the daemon is running
dayshift daemon status

# Stop the daemon
dayshift daemon stop
```

---

## `dayshift labels`

Manage dayshift labels on GitHub repositories.

### `dayshift labels setup`

Create all 13 dayshift labels on configured repositories. Labels that already exist are skipped.

```
dayshift labels setup [flags]
```

| Flag | Type | Default | Description |
|---|---|---|---|
| `--repo` | string | | Target a specific repository (`owner/repo`) instead of all configured repos |

### Examples

```bash
# Create labels on all configured repos
dayshift labels setup

# Create labels on a single repo
dayshift labels setup --repo owner/repo
```

---

## Common Workflows

### First-time setup

```bash
dayshift init                     # Generate config
# Edit ~/.config/dayshift/config.yaml to add your projects
dayshift doctor                   # Verify everything is working
dayshift labels setup             # Create labels on your repos
```

### Process a single issue

```bash
dayshift run --issue 42 --repo owner/repo --yes
```

### Run as a background service

```bash
dayshift daemon start
dayshift daemon status            # Confirm it's running
# ... later ...
dayshift daemon stop
```

### Check on progress

```bash
dayshift status                   # Overview of all issues
dayshift status --issue 42 --repo owner/repo  # Detail for one issue
```

### Retry after an error

On the GitHub issue, remove the `dayshift:error` label and re-add the `dayshift` label, then:

```bash
dayshift run --issue 42 --repo owner/repo --yes
```

### Preview without executing

```bash
dayshift run --dry-run
```
