package pipeline

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/marcus/dayshift/internal/agents"
	"github.com/marcus/dayshift/internal/comments"
	"github.com/marcus/dayshift/internal/scanner"
	"github.com/marcus/dayshift/internal/state"
)

func buildImplementPrompt(issue scanner.PendingWork, research, plan, branch string) string {
	return fmt.Sprintf(`You are an implementation agent. Implement the approved plan for this GitHub issue.

## Issue
Title: %s
Number: #%d
Repository: %s
URL: https://github.com/%s/issues/%d

## Issue Body
%s

## Research
%s

## Approved Plan
%s

## Instructions
0. Work on a new branch. Never work directly on the primary branch.
   Create your feature branch from '%s'.
1. Before creating your branch, record the current branch name.
2. Implement the plan step by step.
3. Ensure tests pass.
4. Create a PR linking to the issue (include "Fixes #%d" or "Closes #%d" in the PR description).
5. Include these git trailers in your commits:
   Dayshift-Issue: https://github.com/%s/issues/%d
   Dayshift-Ref: https://github.com/marcus/dayshift
6. After the PR is submitted, switch back to the original branch.
7. Output a summary of what was implemented.`,
		issue.Issue.Title,
		issue.Issue.Number,
		issue.Project.Repo,
		issue.Project.Repo, issue.Issue.Number,
		issue.Issue.Body,
		research,
		plan,
		branch,
		issue.Issue.Number, issue.Issue.Number,
		issue.Project.Repo, issue.Issue.Number,
	)
}

func (e *Executor) executeImplement(ctx context.Context, work scanner.PendingWork, issueState *state.IssueState) error {
	// Transition to implement phase
	if issueState.Phase == state.PhaseApprove {
		if err := e.state.TransitionPhase(issueState.ID, state.PhaseApprove, state.PhaseImplement, "starting implementation"); err != nil {
			return fmt.Errorf("transition to implement: %w", err)
		}
	}

	e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:implementing")

	// Get research and plan from comments
	research := e.getResearchFromComments(ctx, work)
	plan := e.getPlanFromComments(ctx, work)

	// Detect current branch
	branch := currentBranch(ctx, work.Project.Path)

	// Build and execute prompt
	prompt := buildImplementPrompt(work, research, plan, branch)
	result, err := e.agent.Execute(ctx, agents.ExecuteOptions{
		Prompt:  prompt,
		WorkDir: work.Project.Path,
	})
	if err != nil {
		e.handlePhaseError(ctx, work, issueState, "implement", err)
		return fmt.Errorf("execute implement: %w", err)
	}
	if !result.IsSuccess() {
		e.handlePhaseError(ctx, work, issueState, "implement", fmt.Errorf("agent failed: %s", result.Error))
		return fmt.Errorf("implement agent failed: %s", result.Error)
	}

	// Extract PR URL
	prURL := extractPRURL(result.Output)
	if prURL != "" {
		e.state.SetPRURL(issueState.ID, prURL)
	}

	// Post implementation summary
	summary := fmt.Sprintf("## Implementation Complete\n\n%s", result.Output)
	if prURL != "" {
		summary += fmt.Sprintf("\n\n**PR**: %s", prURL)
	}
	e.github.PostComment(ctx, work.Project.Repo, work.Issue.Number, summary)
	e.state.RecordComment(issueState.ID, state.PhaseImplement, 0, result.Output, "dayshift")

	// Add label and transition
	e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:implemented")
	if err := e.state.TransitionPhase(issueState.ID, state.PhaseImplement, state.PhaseValidate, "implementation complete"); err != nil {
		return fmt.Errorf("transition to validate: %w", err)
	}

	e.logger.Infof("implementation complete for %s#%d (PR: %s)", work.Project.Repo, work.Issue.Number, prURL)
	return nil
}

// getPlanFromComments finds the plan comment on the issue.
func (e *Executor) getPlanFromComments(ctx context.Context, work scanner.PendingWork) string {
	ghComments, err := e.github.GetComments(ctx, work.Project.Repo, work.Issue.Number)
	if err != nil {
		return "(plan not available)"
	}
	var bodies []string
	for _, c := range ghComments {
		bodies = append(bodies, c.Body)
	}
	body, found := comments.FindMarkedComment(bodies, comments.MarkerPlan)
	if !found {
		return "(no plan found)"
	}
	content, _ := comments.ExtractMarkedContent(body, comments.MarkerPlan, comments.MarkerPlanEnd)
	return content
}

// currentBranch returns the current git branch name.
func currentBranch(ctx context.Context, workDir string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(string(output))
}
