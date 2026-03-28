// Package pipeline implements the issue processing pipeline phases.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/marcus/dayshift/internal/agents"
	"github.com/marcus/dayshift/internal/config"
	gh "github.com/marcus/dayshift/internal/github"
	"github.com/marcus/dayshift/internal/logging"
	"github.com/marcus/dayshift/internal/scanner"
	"github.com/marcus/dayshift/internal/state"
)

// Executor processes issues through pipeline phases.
type Executor struct {
	agent  agents.Agent
	github *gh.Client
	state  *state.Manager
	config *config.Config
	logger *logging.Logger
}

// ExecutorOption configures an Executor.
type ExecutorOption func(*Executor)

func WithAgent(a agents.Agent) ExecutorOption {
	return func(e *Executor) { e.agent = a }
}

func WithGitHub(c *gh.Client) ExecutorOption {
	return func(e *Executor) { e.github = c }
}

func WithState(m *state.Manager) ExecutorOption {
	return func(e *Executor) { e.state = m }
}

func WithConfig(c *config.Config) ExecutorOption {
	return func(e *Executor) { e.config = c }
}

// NewExecutor creates a new pipeline Executor.
func NewExecutor(opts ...ExecutorOption) *Executor {
	e := &Executor{
		logger: logging.Component("pipeline"),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ProcessIssue dispatches an issue to the appropriate phase handler.
func (e *Executor) ProcessIssue(ctx context.Context, work scanner.PendingWork) error {
	e.logger.Infof("processing %s#%d → phase %s (reason: %s)",
		work.Project.Repo, work.Issue.Number, work.NextPhase, work.Reason)

	// Ensure issue is tracked in state
	issueState := work.IssueState
	if issueState == nil {
		id, err := e.state.UpsertIssue(&state.IssueState{
			Repo:        work.Project.Repo,
			IssueNumber: work.Issue.Number,
			Title:       work.Issue.Title,
			Phase:       state.PhasePending,
		})
		if err != nil {
			return fmt.Errorf("create issue state: %w", err)
		}
		issueState, err = e.state.GetIssue(work.Project.Repo, work.Issue.Number)
		if err != nil || issueState == nil {
			return fmt.Errorf("get issue state after create (id=%d): %w", id, err)
		}
	}

	switch work.NextPhase {
	case state.PhaseResearch:
		return e.executeResearch(ctx, work, issueState)
	case state.PhasePlan:
		return e.executePlan(ctx, work, issueState)
	case state.PhaseApprove:
		return e.executeApprove(ctx, work, issueState)
	case state.PhaseImplement:
		return e.executeImplement(ctx, work, issueState)
	case state.PhaseValidate:
		return e.executeValidate(ctx, work, issueState)
	default:
		return fmt.Errorf("unknown phase: %s", work.NextPhase)
	}
}

// prURLPattern matches GitHub PR URLs.
var prURLPattern = regexp.MustCompile(`https://github\.com/[^/\s]+/[^/\s]+/pull/\d+`)

// extractPRURL finds the last PR URL in text (to get the actual PR, not referenced ones).
func extractPRURL(text string) string {
	matches := prURLPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

// cleanAgentOutput strips agent "thinking out loud" preamble from output.
// Agents often emit status messages before the actual document. We find
// the first markdown heading (# ...) or horizontal rule (---) and discard
// everything before it.
func cleanAgentOutput(output string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") || (trimmed == "---" && i < len(lines)-1) {
			return strings.Join(lines[i:], "\n")
		}
	}
	// No heading found — return as-is
	return output
}

// getSessionID extracts the Copilot session ID from phase_data JSON.
func getSessionID(phaseData string) string {
	if phaseData == "" {
		return ""
	}
	var data struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(phaseData), &data); err != nil {
		return ""
	}
	return data.SessionID
}
