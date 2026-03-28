package pipeline

import (
	"context"
	"fmt"

	"github.com/marcus/dayshift/internal/scanner"
	"github.com/marcus/dayshift/internal/state"
)

func (e *Executor) executeApprove(ctx context.Context, work scanner.PendingWork, issueState *state.IssueState) error {
	// Check if auto-approve is enabled
	if e.config != nil && e.config.Phases.Approve.AutoApprove {
		e.logger.Infof("auto-approving %s#%d", work.Project.Repo, work.Issue.Number)
		// Skip straight to implement
		if issueState.Phase == state.PhaseApprove {
			if err := e.state.TransitionPhase(issueState.ID, state.PhaseApprove, state.PhaseImplement, "auto-approved"); err != nil {
				return fmt.Errorf("transition to implement: %w", err)
			}
		}
		return nil
	}

	// Post approval request comment
	body := fmt.Sprintf(`## ✅ Plan Ready for Approval

The implementation plan for this issue is complete and ready for review.

**To proceed with implementation**, add the %sdayshift:approved%s label to this issue.

**To request changes**, reply with your feedback and the plan will be updated.`,
		"`", "`")

	if err := e.github.PostComment(ctx, work.Project.Repo, work.Issue.Number, body); err != nil {
		return fmt.Errorf("post approval comment: %w", err)
	}

	// Add awaiting-approval label (and planned if coming from clarify)
	e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:planned")
	e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:awaiting-approval")
	e.github.RemoveLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:needs-input")

	// Ensure we're in the approve phase
	if issueState.Phase != state.PhaseApprove {
		if err := e.state.TransitionPhase(issueState.ID, issueState.Phase, state.PhaseApprove, "awaiting human approval"); err != nil {
			// May already be in approve phase from plan transition
			e.logger.Debugf("transition to approve: %v", err)
		}
	}

	e.logger.Infof("awaiting approval for %s#%d", work.Project.Repo, work.Issue.Number)
	return nil
}
