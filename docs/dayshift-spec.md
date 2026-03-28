# Dayshift: Issue-Driven AI Agent Pipeline

> **Status:** Draft Specification  
> **Author:** Generated from nightshift codebase analysis  
> **Scope:** Separate binary/repo, sibling to nightshift

---

## 1. Problem Statement

Nightshift excels at autonomous overnight tasks — it picks work from a registry, runs plan→implement→review loops, and produces PRs while you sleep. But there's a class of work that doesn't fit this model: **GitHub issues that need research, planning, human input, and iterative refinement before implementation**.

Today, a developer must:
1. Read each issue manually
2. Research the codebase for relevant context
3. Draft an implementation plan
4. Get feedback from stakeholders
5. Implement and validate

**Dayshift** automates this pipeline by processing GitHub issues through structured phases, pausing for human input when needed, and resuming autonomously when the human responds.

---

## 2. Decision: Separate Tool vs. Extending Nightshift

### 2.1 Why Not Extend Nightshift?

| Dimension | Nightshift | Dayshift |
|---|---|---|
| **Execution model** | Autonomous, unattended | Human-in-the-loop, resumable |
| **Work source** | Task registry (built-in + custom) | GitHub issues |
| **Loop** | Plan → Implement → Review (single run) | Multi-phase state machine with pauses |
| **Timing** | Off-hours window (23:00–07:00) | Anytime; triggered by issue activity |
| **State** | Stateless per-run (cooldowns only) | Persistent per-issue phase tracking |
| **Human interaction** | None (fully autonomous) | Labels + comments for async Q&A |
| **Output** | PRs, reports, analysis comments | Research docs, plans, PRs — all on the issue |

These are fundamentally different execution models. Bolting a state machine with human-in-the-loop pauses onto nightshift's single-run autonomous loop would:
- Complicate the orchestrator with branching execution paths
- Require persistent issue state that doesn't fit nightshift's cooldown model
- Blur the conceptual boundary: "nightshift works while you sleep, dayshift works while you're awake"

### 2.2 What Can Be Reused from Nightshift

Analysis of nightshift's `internal/` packages reveals clear reuse opportunities:

| Package | Reusability | Notes |
|---|---|---|
| `internal/agents/` | **Directly reusable** | Clean `Agent` interface; `CopilotAgent`, `ClaudeAgent`, `CodexAgent` are prompt-agnostic |
| `internal/providers/` | **Directly reusable** | Usage telemetry reading (token counts from provider data dirs) |
| `internal/budget/` | **Minor refactoring** | Generic token budget algorithm; coupled to `config.Config` struct |
| `internal/orchestrator/` | **Minor refactoring** | Plan→implement→review loop is generic; prompts have nightshift branding |
| `internal/integrations/github.go` | **Minor refactoring** | `GitHubReader` with `Read()`, `Comment()`, `Close()` — label is configurable |
| `internal/logging/` | **Minor refactoring** | Replace hardcoded `nightshift-` filename prefix |
| `internal/db/` | **Reusable pattern** | SQLite wrapper is generic; migrations are nightshift-specific |
| `internal/config/` | **Pattern only** | Viper+YAML pattern reusable; schema is nightshift-specific |
| `internal/state/` | **Not reusable** | Nightshift-specific project/task cooldown tracking |
| `cmd/` | **Pattern only** | Cobra CLI scaffolding; all commands are nightshift-specific |

### 2.3 Recommendation

**Separate repo and binary** with shared Go module dependencies where practical. Initially, copy-adapt the reusable packages (`agents`, `providers`, `logging`). Once both tools stabilize, consider extracting a shared `github.com/marcus/shiftkit` library for the truly generic pieces (agent interface, budget algorithm, GitHub operations).

---

## 3. Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                     dayshift CLI                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐             │
│  │  daemon   │  │   run    │  │  status  │  ...        │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘             │
│       │              │              │                    │
│  ┌────▼──────────────▼──────────────▼────────────────┐  │
│  │              Issue Processor                       │  │
│  │  ┌─────────┐  ┌─────────┐  ┌──────────────────┐  │  │
│  │  │ Scanner │  │ Router  │  │  Phase Executor   │  │  │
│  │  │ (gh)    │──▶│ (state) │──▶│  (skill runner)  │  │  │
│  │  └─────────┘  └─────────┘  └──────────────────┘  │  │
│  └───────────────────────────────────────────────────┘  │
│                          │                               │
│  ┌───────────────────────▼───────────────────────────┐  │
│  │              Shared Infrastructure                 │  │
│  │  ┌────────┐  ┌────────┐  ┌────────┐  ┌────────┐  │  │
│  │  │ Agents │  │ Budget │  │ GitHub │  │ State  │  │  │
│  │  │(copilot│  │tracker │  │ (gh)   │  │(sqlite)│  │  │
│  │  │ claude)│  │        │  │        │  │        │  │  │
│  │  └────────┘  └────────┘  └────────┘  └────────┘  │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### 3.1 Core Components

1. **Issue Scanner** — polls GitHub for issues matching configured labels; detects new issues and label changes on existing ones
2. **State Router** — reads per-issue state from SQLite, determines which phase to execute next
3. **Phase Executor** — runs the appropriate skill for the current phase, posts results, transitions state
4. **Label Manager** — adds/removes labels to signal phase transitions and human-input requests
5. **Comment Parser** — reads human responses from issue comments to extract answers to questions

---

## 4. Issue Pipeline: Phases

Each issue progresses through a 5-phase pipeline. Phase transitions are tracked in SQLite and signaled via GitHub labels.

```
┌──────────┐     ┌──────────┐     ┌───────────┐     ┌───────────┐     ┌──────────┐
│ Research  │────▶│   Plan   │────▶│  Approve  │────▶│ Implement │────▶│ Validate │
│ (auto)   │     │ (auto)   │     │ (human)   │     │ (auto)    │     │ (auto)   │
└──────────┘     └─────┬────┘     └───────────┘     └───────────┘     └──────────┘
                       │                ▲
                       ▼                │
                 ┌───────────┐          │
                 │  Clarify  │──────────┘
                 │ (human)   │  (iterate until all questions answered)
                 └───────────┘
```

### 4.1 Phase Details

#### Phase 1: Research
- **Trigger:** New issue with `dayshift` label (no prior dayshift comments)
- **Skill:** `/research-codebase <issue-url>`
- **Output:** Research document posted as issue comment with `<!-- dayshift:research -->` marker
- **Transition:** → Phase 2 (Plan)
- **Label changes:** Add `dayshift:researched`

#### Phase 2: Plan
- **Trigger:** Issue has research comment, no plan comment yet
- **Skill:** `/create-plan` (autonomous variant — see §5)
- **Input:** Issue body + research comment
- **Output:** Implementation plan posted as comment with `<!-- dayshift:plan -->` marker
- **Key behavior:** If the plan agent identifies questions/decisions that need human input, it appends a structured "Questions" section (see §4.2)
- **Transition:** If questions → Phase 2a (Clarify); if no questions → Phase 3 (Approve)
- **Label changes:** Add `dayshift:planned`; if questions, also add `dayshift:needs-input`

#### Phase 2a: Clarify (iterative)
- **Trigger:** Issue has `dayshift:needs-input` label AND a new human comment since the last dayshift comment
- **Action:** Parse human answers, re-run plan with updated context
- **Transition:** If more questions → stay in Clarify; if all resolved → Phase 3 (Approve)
- **Label changes:** Remove `dayshift:needs-input` when resolved; add if new questions arise

#### Phase 3: Approve
- **Trigger:** Plan is complete (no outstanding questions)
- **Action:** Post approval request comment: "Plan is ready for implementation. Add `dayshift:approved` label to proceed."
- **Label changes:** Add `dayshift:awaiting-approval`
- **Transition:** Human adds `dayshift:approved` label → Phase 4

#### Phase 4: Implement
- **Trigger:** Issue has `dayshift:approved` label
- **Skill:** `/implement-plan` (with plan from Phase 2)
- **Output:** Creates a branch and PR linked to the issue
- **Transition:** → Phase 5 (Validate)
- **Label changes:** Add `dayshift:implementing`, then `dayshift:implemented`

#### Phase 5: Validate
- **Trigger:** PR exists, implementation complete
- **Skill:** `/validate-plan` (checks implementation against plan)
- **Output:** Validation report posted as comment on the PR and the issue
- **Transition:** If passed → mark issue `dayshift:complete`; if failed → post issues and optionally re-enter Phase 4
- **Label changes:** Add `dayshift:validated` or `dayshift:needs-fixes`

### 4.2 Structured Questions Format

When the plan phase identifies decisions that need human input, it posts them in a parseable format:

```markdown
<!-- dayshift:questions -->
## Questions for Human Review

The following decisions need your input before implementation can proceed.
Reply in a comment with your answers (reference by number).

### 1. Database choice
The feature requires persistent storage. Which database should we use?
- [ ] PostgreSQL (recommended based on existing infra)
- [ ] SQLite (simpler, no external dependency)
- [ ] Redis (if data is ephemeral)

### 2. Error handling strategy
How should we handle upstream API failures?
- [ ] Retry with exponential backoff (recommended)
- [ ] Fail fast and notify user
- [ ] Queue for manual retry

<!-- /dayshift:questions -->
```

The **Comment Parser** detects human replies by:
1. Looking for comments posted after the questions comment
2. Matching numbered references ("1: PostgreSQL", "For #1, go with PostgreSQL")
3. Detecting checkbox toggles (if the human edits the original comment)

---

## 5. Skill Requirements

Dayshift relies on Copilot CLI skills at each phase. Some existing skills need adaptation for autonomous (non-interactive) use.

### 5.1 Existing Skills (usable as-is or with minor changes)

| Phase | Skill | Changes Needed |
|---|---|---|
| Research | `research-codebase` | None — already outputs a document. Just need to pass issue URL. |
| Validate | `validate-plan` | None — already checks implementation against plan. |

### 5.2 Skills Needing Autonomous Variants

| Phase | Skill | Problem | Solution |
|---|---|---|---|
| Plan | `create-plan` | Uses `ask_user` for interactive Q&A | Create `create-plan-async` variant that collects all questions and emits them as a structured block instead of asking interactively. Agent runs with `--no-ask-user`. |
| Implement | `implement-plan` | May use `ask_user` for ambiguous decisions | Create `implement-plan-async` variant that makes reasonable defaults and documents assumptions. Falls back to labeling for human input if truly blocked. |

### 5.3 New Skills Needed

| Skill | Purpose |
|---|---|
| `parse-human-response` | Parses human comment replies to extract answers to structured questions. Maps answers back to question IDs. |
| `update-plan` | Takes an existing plan + human answers and produces an updated plan. Similar to `iterate-plan` but driven by Q&A data. |

### 5.4 Skill Invocation Pattern

Dayshift invokes skills by constructing prompts that include the skill command:

```
copilot -p "/research-codebase https://github.com/org/repo/issues/42" \
  --no-ask-user --silent
```

For phases that need issue context, dayshift prepends the issue body and prior comments to the prompt.

---

## 6. Label Protocol

Labels are the state machine's external interface. They're visible in GitHub UI and enable both automation and manual override.

### 6.1 Label Definitions

| Label | Meaning | Set By |
|---|---|---|
| `dayshift` | Issue should be processed by dayshift | Human |
| `dayshift:researched` | Research phase complete | Agent |
| `dayshift:planned` | Plan phase complete | Agent |
| `dayshift:needs-input` | Agent needs human answers to proceed | Agent |
| `dayshift:awaiting-approval` | Plan ready, waiting for human approval | Agent |
| `dayshift:approved` | Human approved the plan for implementation | Human |
| `dayshift:implementing` | Implementation in progress | Agent |
| `dayshift:implemented` | Implementation complete, PR created | Agent |
| `dayshift:validated` | Validation passed | Agent |
| `dayshift:needs-fixes` | Validation found issues | Agent |
| `dayshift:complete` | All phases done | Agent |
| `dayshift:error` | Processing failed (check comments) | Agent |
| `dayshift:paused` | Human manually paused processing | Human |

### 6.2 Manual Overrides

- **Pause:** Add `dayshift:paused` to stop processing. Remove to resume.
- **Skip phase:** Manually add a phase-complete label (e.g., `dayshift:planned`) to skip it.
- **Restart:** Remove all `dayshift:*` labels except `dayshift` to restart from scratch.
- **Cancel:** Remove the `dayshift` label entirely.

---

## 7. State Management

### 7.1 SQLite Schema

```sql
CREATE TABLE issues (
    id            INTEGER PRIMARY KEY,
    repo          TEXT NOT NULL,           -- "owner/repo"
    issue_number  INTEGER NOT NULL,
    title         TEXT,
    phase         TEXT NOT NULL DEFAULT 'pending',
    -- phases: pending/research/plan/clarify/approve/implement/validate/complete/error
    phase_started DATETIME,
    phase_data    TEXT,                    -- JSON: phase-specific context
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(repo, issue_number)
);

CREATE TABLE issue_comments (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id      INTEGER REFERENCES issues(id),
    phase         TEXT NOT NULL,
    comment_id    INTEGER,                -- GitHub comment ID
    content       TEXT,                   -- Comment body (cached)
    author        TEXT,                   -- "dayshift" or GitHub username
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE phase_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id      INTEGER REFERENCES issues(id),
    from_phase    TEXT,
    to_phase      TEXT,
    reason        TEXT,                   -- Why the transition happened
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 7.2 State Recovery

On startup, dayshift reconciles local SQLite state with GitHub label state:
- If GitHub has labels that SQLite doesn't know about (e.g., human added `dayshift:approved`), update SQLite
- If SQLite shows a phase but GitHub labels are inconsistent, trust GitHub labels (they're the source of truth for human actions)

---

## 8. Configuration

```yaml
# ~/.config/dayshift/config.yaml
repo: owner/repo                    # Target repository

schedule:
  poll_interval: 5m                 # How often to check for issue changes
  
labels:
  trigger: dayshift                 # Label that activates processing
  prefix: "dayshift:"              # Prefix for phase labels

provider:
  preference: [copilot, claude]    # Agent preference order
  timeout: 30m                     # Per-phase timeout

budget:
  mode: daily
  max_percent: 100
  billing_mode: subscription

phases:
  research:
    enabled: true
    skill: research-codebase
  plan:
    enabled: true
    skill: create-plan              # Will use autonomous variant
    max_clarify_rounds: 3          # Max Q&A iterations before escalating
  approve:
    enabled: true
    auto_approve: false             # If true, skip human approval
  implement:
    enabled: true
    skill: implement-plan
  validate:
    enabled: true
    skill: validate-plan

logging:
  level: info
  path: ~/.local/share/dayshift/logs
```

---

## 9. CLI Commands

```
dayshift                          # Show help
dayshift init                     # Create config file
dayshift run                      # Process issues once (manual trigger)
dayshift run --issue 42           # Process a specific issue
dayshift run --phase research     # Run only the research phase
dayshift daemon start             # Start background polling
dayshift daemon stop              # Stop background polling
dayshift status                   # Show issue processing status
dayshift status --issue 42        # Show detailed status for an issue
dayshift labels setup             # Create all dayshift labels in the repo
dayshift doctor                   # Check config, gh auth, labels, etc.
```

---

## 10. Implementation Roadmap

### Phase A: Foundation
- Project scaffolding (Go module, Cobra CLI, config loading)
- GitHub operations (list issues, read comments, post comments, manage labels)
- SQLite state management (issue tracking, phase transitions)
- Label setup command (`dayshift labels setup`)

### Phase B: Research Pipeline
- Issue scanner (find `dayshift`-labeled issues without research)
- Research phase executor (invoke `/research-codebase`)
- Comment posting with markers
- Manual `dayshift run --issue N --phase research`

### Phase C: Plan Pipeline  
- Autonomous plan skill variant (no `ask_user`, emits questions block)
- Plan phase executor
- Structured question format + comment parser
- Clarify loop (detect human replies, re-run plan)
- Human approval gate

### Phase D: Implementation Pipeline
- Implement phase executor (invoke `/implement-plan`, create PR)
- Validate phase executor (invoke `/validate-plan`)
- PR linking to issue

### Phase E: Daemon + Polish
- Background polling daemon
- State reconciliation (SQLite ↔ GitHub labels)
- Budget tracking integration
- Error handling + `dayshift:error` label
- Reporting (`dayshift status`, `dayshift report`)

---

## 11. Open Questions

1. **Shared module extraction timing** — Should we extract `shiftkit` before starting dayshift, or copy-adapt and extract later? (Recommendation: copy-adapt first, extract when patterns stabilize.)

2. **Multi-repo support** — The spec is single-repo for v1. Supporting multiple repos would require per-repo config and state partitioning. Defer to v2.

3. **Rate limiting** — How aggressively should dayshift process issues? One at a time? Parallel? Need to balance thoroughness with API/budget limits.

4. **Skill versioning** — Autonomous skill variants (`create-plan-async`) need to be maintained alongside interactive versions. Should they be separate skills or mode flags?

5. **Comment length limits** — GitHub comments have a ~65,536 character limit. Research documents and plans might exceed this. Strategy: split into multiple comments with part indicators, or post as gist links.

6. **Security** — Dayshift will execute code based on issue content. Issues are attacker-controllable input. Need prompt injection mitigations (input sanitization, sandboxed execution, review before implement).
