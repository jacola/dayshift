package pipeline

import (
	"context"
	"fmt"

	"github.com/marcus/dayshift/internal/scanner"
	"github.com/marcus/dayshift/internal/state"
)

// handlePhaseError posts an error comment and transitions to error state.
func (e *Executor) handlePhaseError(ctx context.Context, work scanner.PendingWork, issueState *state.IssueState, phase string, phaseErr error) {
	errMsg := fmt.Sprintf("## ❌ Dayshift Error: %s phase\n\n```\n%s\n```\n\nThe issue has been marked with `dayshift:error`. Remove the label and add `dayshift` to retry.",
		phase, phaseErr.Error())

	e.github.PostComment(ctx, work.Project.Repo, work.Issue.Number, errMsg)
	e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:error")

	// Try to transition to error state
	e.state.TransitionPhase(issueState.ID, issueState.Phase, state.PhaseError, fmt.Sprintf("%s error: %v", phase, phaseErr))

	e.logger.Errorf("%s phase failed for %s#%d: %v", phase, work.Project.Repo, work.Issue.Number, phaseErr)
}
