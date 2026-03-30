# Configuration Reference

Dayshift is configured through a YAML file at `~/.config/dayshift/config.yaml`. Run `dayshift init` to generate a starter config.

All settings can be overridden with environment variables using the `DAYSHIFT_` prefix (see [Environment Variables](#environment-variables)).

## Full Annotated Config

```yaml
# ── Projects ─────────────────────────────────────────────────────────
# Repositories to monitor for labeled issues.
projects:
  - repo: owner/repo           # GitHub repository (owner/repo format, required)
    path: ~/code/repo           # Local checkout path (required, ~ is expanded)
    priority: 10                # Higher = processed first when multiple issues are pending

  - repo: owner/another-repo
    path: ~/code/another-repo
    priority: 5

# ── Schedule ─────────────────────────────────────────────────────────
# Controls polling behavior in daemon mode.
schedule:
  poll_interval: 5m             # How often to scan for new work (Go duration string)

# ── Labels ───────────────────────────────────────────────────────────
# Customize the labels dayshift uses on GitHub issues.
labels:
  trigger: dayshift             # Label that activates processing for an issue
  prefix: "dayshift:"           # Prefix for all phase-tracking labels

# ── Provider ─────────────────────────────────────────────────────────
# AI agent configuration.
provider:
  preference:                   # Provider selection order (first available wins)
    - claude
    - copilot
    - codex
  timeout: 30m                  # Per-phase execution timeout (Go duration string)

  claude:
    enabled: true               # Whether to consider Claude as a provider
    data_path: ~/.claude         # Claude data directory
    dangerously_skip_permissions: true  # Pass --dangerously-skip-permissions to claude CLI

  codex:
    enabled: true               # Whether to consider Codex as a provider
    data_path: ~/.codex          # Codex data directory
    dangerously_bypass_approvals_and_sandbox: true  # Pass --dangerously-bypass-approvals-and-sandbox

  copilot:
    enabled: true               # Whether to consider Copilot as a provider
    data_path: ~/.copilot        # Copilot data directory

# ── Budget ───────────────────────────────────────────────────────────
# Token budget controls (configured but not yet enforced).
budget:
  mode: daily                   # Budget period: "daily" or "weekly"
  max_percent: 100              # Max percentage of budget to use per run (0-100)

# ── Phases ───────────────────────────────────────────────────────────
# Per-phase settings.
phases:
  research:
    enabled: true               # Set to false to skip research

  plan:
    enabled: true               # Set to false to skip planning
    max_clarify_rounds: 3       # Max question/answer loops before escalating (≥ 0)

  implement:
    enabled: true               # Set to false to skip implementation

  validate:
    enabled: true               # Set to false to skip validation

# ── Logging ──────────────────────────────────────────────────────────
# Logging configuration.
logging:
  level: info                   # Log level: debug, info, warn, error
  path: ~/.local/share/dayshift/logs  # Log directory (~ is expanded)
  format: json                  # Output format: json or text
```

## Section Details

### Projects

Each entry represents a GitHub repository that dayshift will monitor for labeled issues.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `repo` | string | ✅ | — | GitHub repository in `owner/repo` format. Must contain a `/`. |
| `path` | string | ✅ | — | Local filesystem path to the repository checkout. Tilde (`~`) is expanded to the home directory. |
| `priority` | int | ❌ | `0` | Processing priority. Higher values are processed first when multiple issues are pending across repos. |

When multiple issues are pending, they're sorted by project priority (descending), then by issue age (oldest first).

### Schedule

| Field | Type | Default | Description |
|---|---|---|---|
| `poll_interval` | string | `"5m"` | How often the daemon polls GitHub for new work. Must be a valid Go duration string (e.g., `30s`, `5m`, `1h`). |

This only applies when running in daemon mode (`dayshift daemon start`). Manual runs (`dayshift run`) execute immediately.

### Labels

| Field | Type | Default | Description |
|---|---|---|---|
| `trigger` | string | `"dayshift"` | The label that activates dayshift processing for an issue. Add this label to an issue to start the pipeline. |
| `prefix` | string | `"dayshift:"` | Prefix for all phase-tracking labels. Change this if you need to avoid conflicts with existing labels. |

For example, with the defaults, the full set of labels would be `dayshift`, `dayshift:researched`, `dayshift:planned`, etc.

### Provider

Controls which AI agent is used and how it's invoked.

| Field | Type | Default | Description |
|---|---|---|---|
| `preference` | []string | `["claude", "copilot", "codex"]` | Ordered list of providers to try. The first available provider is used. Valid values: `claude`, `codex`, `copilot`. |
| `timeout` | string | `"30m"` | Maximum time for a single phase execution. Must be a valid Go duration string. |

#### Provider-Specific Settings

Each provider has its own configuration block:

**Claude** (`provider.claude`)

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Include Claude in provider selection. |
| `data_path` | string | `"~/.claude"` | Claude data directory. |
| `dangerously_skip_permissions` | bool | `true` | Pass `--dangerously-skip-permissions` flag to the `claude` CLI. Required for non-interactive use. |

**Codex** (`provider.codex`)

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Include Codex in provider selection. |
| `data_path` | string | `"~/.codex"` | Codex data directory. |
| `dangerously_bypass_approvals_and_sandbox` | bool | `true` | Pass `--dangerously-bypass-approvals-and-sandbox` flag to the `codex` CLI. Required for non-interactive use. |

**Copilot** (`provider.copilot`)

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Include Copilot in provider selection. |
| `data_path` | string | `"~/.copilot"` | Copilot data directory. |

Copilot is invoked as a `gh` extension (`gh copilot`) by default. If a standalone `copilot` binary is found, it's preferred. Copilot is the only provider that supports session resume across phases.

### Budget

| Field | Type | Default | Description |
|---|---|---|---|
| `mode` | string | `"daily"` | Budget tracking period. Valid values: `daily`, `weekly`. |
| `max_percent` | int | `100` | Maximum percentage of the token budget to use per run. Must be between 0 and 100. |

> **Note:** Budget fields are parsed and validated but not yet enforced at runtime.

### Phases

Each phase can be individually configured. Disabling a phase skips it in the pipeline.

| Field | Type | Applies To | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | All phases | `true` | Whether the phase executes. |
| `max_clarify_rounds` | int | `plan` only | `3` | Maximum question/answer loops before the pipeline stops asking. Must be ≥ 0. |

### Logging

| Field | Type | Default | Description |
|---|---|---|---|
| `level` | string | `"info"` | Minimum log level. Valid values: `debug`, `info`, `warn`, `error`. |
| `path` | string | `"~/.local/share/dayshift/logs"` | Directory for log files. Tilde is expanded. |
| `format` | string | `"json"` | Log output format. Valid values: `json`, `text`. |

Logs are written to daily files named `dayshift-YYYY-MM-DD.log` with automatic 7-day retention.

## Environment Variables

All config values can be overridden with environment variables using the `DAYSHIFT_` prefix. Nested keys use underscores as separators.

| Config Path | Environment Variable |
|---|---|
| `schedule.poll_interval` | `DAYSHIFT_SCHEDULE_POLL_INTERVAL` |
| `labels.trigger` | `DAYSHIFT_LABELS_TRIGGER` |
| `provider.timeout` | `DAYSHIFT_PROVIDER_TIMEOUT` |
| `logging.level` | `DAYSHIFT_LOGGING_LEVEL` |

Environment variables take precedence over values in the config file.

## Validation Rules

The config is validated on load. Invalid configs produce a clear error message.

- `poll_interval` and `timeout` must be valid Go duration strings
- `budget.max_percent` must be between 0 and 100
- Provider names in `preference` must be `claude`, `codex`, or `copilot`
- Each project must have both `repo` (containing `/`) and `path`
- `max_clarify_rounds` must be ≥ 0
- `logging.level` must be `debug`, `info`, `warn`, or `error`
- `logging.format` must be `json` or `text`

## File Paths

| Purpose | Default Path |
|---|---|
| Config file | `~/.config/dayshift/config.yaml` |
| SQLite database | `~/.local/share/dayshift/dayshift.db` |
| Daemon PID file | `~/.local/share/dayshift/dayshift.pid` |
| Log directory | `~/.local/share/dayshift/logs/` |
