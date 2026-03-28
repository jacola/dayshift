package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/marcus/dayshift/internal/agents"
	"github.com/marcus/dayshift/internal/comments"
	"github.com/marcus/dayshift/internal/scanner"
	"github.com/marcus/dayshift/internal/state"
)

func buildValidatePrompt(issue scanner.PendingWork, plan, prURL string) string {
	return fmt.Sprintf(`You are a validation agent. Verify that the implementation correctly addresses the issue.

## Issue
Title: %s
Number: #%d
Repository: %s

## Issue Body
%s

## Approved Plan
%s

## Implementation PR
%s

## Instructions
1. Review the PR changes against the plan
2. Verify the implementation addresses the issue requirements
3. Check for obvious bugs or issues
4. Verify tests pass if applicable
5. Summarize your findings

Output your validation as a structured report:
- PASSED or FAILED
- What was verified
- Any issues found
- Recommendations`,
		issue.Issue.Title,
		issue.Issue.Number,
		issue.Project.Repo,
		issue.Issue.Body,
		plan,
		prURL,
	)
}

func (e *Executor) executeValidate(ctx context.Context, work scanner.PendingWork, issueState *state.IssueState) error {
	// Transition to validate phase
	if issueState.Phase == state.PhaseImplement {
		if err := e.state.TransitionPhase(issueState.ID, state.PhaseImplement, state.PhaseValidate, "starting validation"); err != nil {
			return fmt.Errorf("transition to validate: %w", err)
		}
	}

	// Get plan and PR URL
	plan := e.getPlanFromComments(ctx, work)
	prURL := issueState.PRURL

	// Build and execute prompt
	prompt := buildValidatePrompt(work, plan, prURL)
	result, err := e.agent.Execute(ctx, agents.ExecuteOptions{
		Prompt:  prompt,
		WorkDir: work.Project.Path,
	})
	if err != nil {
		e.handlePhaseError(ctx, work, issueState, "validate", err)
		return fmt.Errorf("execute validate: %w", err)
	}
	if !result.IsSuccess() {
		e.handlePhaseError(ctx, work, issueState, "validate", fmt.Errorf("agent failed: %s", result.Error))
		return fmt.Errorf("validate agent failed: %s", result.Error)
	}

	// Post validation report
	commentBody := comments.WrapWithMarker(
		comments.MarkerValidation, comments.MarkerValidationEnd,
		result.Output,
	)
	e.github.PostComment(ctx, work.Project.Repo, work.Issue.Number, commentBody)
	e.state.RecordComment(issueState.ID, state.PhaseValidate, 0, result.Output, "dayshift")

	// Check if validation passed (heuristic)
	passed := inferValidationPassed(result.Output)

	if passed {
		e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:validated")
		e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:complete")
		if err := e.state.TransitionPhase(issueState.ID, state.PhaseValidate, state.PhaseComplete, "validation passed"); err != nil {
			return fmt.Errorf("transition to complete: %w", err)
		}
		e.logger.Infof("validation passed for %s#%d — complete!", work.Project.Repo, work.Issue.Number)
	} else {
		e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:needs-fixes")
		e.logger.Infof("validation failed for %s#%d — needs fixes", work.Project.Repo, work.Issue.Number)
		// Stay in validate phase — could re-enter implement if desired
	}

	return nil
}

// inferValidationPassed uses keyword heuristic to determine if validation passed.
func inferValidationPassed(output string) bool {
	passIndicators := []string{"PASSED", "passed", "approved", "looks good", "lgtm", "no issues", "complete", "correct", "successful"}
	failIndicators := []string{"FAILED", "failed", "rejected", "issues found", "needs work", "incorrect", "bug", "missing", "incomplete"}

	passScore := 0
	failScore := 0

	lower := strings.ToLower(output)
	for _, indicator := range passIndicators {
		if strings.Contains(lower, strings.ToLower(indicator)) {
			passScore++
		}
	}
	for _, indicator := range failIndicators {
		if strings.Contains(lower, strings.ToLower(indicator)) {
			failScore++
		}
	}

	return passScore > failScore
}
