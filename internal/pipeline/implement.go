package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/marcus/dayshift/internal/agents"
	"github.com/marcus/dayshift/internal/comments"
	gh "github.com/marcus/dayshift/internal/github"
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
0. You are already on branch '%s'. Do NOT create a new branch or switch branches.
1. Implement the plan step by step.
2. Ensure tests pass.
3. Commit your changes. Include these git trailers:
   Dayshift-Issue: https://github.com/%s/issues/%d
   Dayshift-Ref: https://github.com/marcus/dayshift
4. Push the branch and create a PR linking to the issue (include "Fixes #%d" or "Closes #%d" in the PR description).
5. Do NOT switch branches after creating the PR — dayshift manages branch checkout.
6. Output a summary of what was implemented.

## PR Description Requirements
The PR description MUST include these sections:
- **Summary**: What was changed and why (reference the issue)
- **Changes**: Bullet list of specific code changes made
- **Manual Testing Steps**: Detailed step-by-step QA instructions so a reviewer can verify the fix works. Be specific — include exact actions to take, expected behavior before and after the fix, and edge cases to test. Write these for an engineer who may not have context on the issue.

Example testing steps format:
### Manual Testing Steps
1. [Precondition setup if needed]
2. [Exact action to perform]
3. [Expected result]
4. [Additional scenarios / edge cases]

## CRITICAL: Output Requirements
- Output your implementation summary directly as your response text
- The full content of your response will be posted as a GitHub comment on the issue`,
		issue.Issue.Title,
		issue.Issue.Number,
		issue.Project.Repo,
		issue.Project.Repo, issue.Issue.Number,
		issue.Issue.Body,
		research,
		plan,
		branch,
		issue.Project.Repo, issue.Issue.Number,
		issue.Issue.Number, issue.Issue.Number,
	)
}

func (e *Executor) executeImplement(ctx context.Context, work scanner.PendingWork, issueState *state.IssueState) error {
	// Transition to implement phase if coming from plan
	if issueState.Phase == state.PhasePlan {
		if err := e.state.TransitionPhase(issueState.ID, state.PhasePlan, state.PhaseImplement, "starting implementation"); err != nil {
			return fmt.Errorf("transition to implement: %w", err)
		}
	}

	e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:implementing")

	// Get research and plan from comments
	research := e.getResearchFromComments(ctx, work)
	plan := e.getPlanFromComments(ctx, work)

	// Save the original branch to switch back after
	originalBranch := currentBranch(ctx, work.Project.Path)

	// Create or checkout the issue branch
	issueBranch, err := ensureIssueBranch(ctx, work.Project.Path, work.Issue.Number, work.Issue.Title)
	if err != nil {
		e.handlePhaseError(ctx, work, issueState, "implement", fmt.Errorf("branch setup: %w", err))
		return fmt.Errorf("branch setup: %w", err)
	}
	e.logger.Infof("working on branch %s for %s#%d", issueBranch, work.Project.Repo, work.Issue.Number)

	// Ensure we switch back to the original branch when done
	defer func() {
		if originalBranch != "" && originalBranch != issueBranch {
			gitCheckout(ctx, work.Project.Path, originalBranch)
		}
	}()

	// Resume session from plan phase
	sessionID := getSessionID(issueState.PhaseData)

	// Build and execute prompt
	prompt := buildImplementPrompt(work, research, plan, issueBranch)
	result, err := e.agent.Execute(ctx, agents.ExecuteOptions{
		Prompt:    prompt,
		WorkDir:   work.Project.Path,
		SessionID: sessionID,
	})
	if err != nil {
		e.handlePhaseError(ctx, work, issueState, "implement", err)
		return fmt.Errorf("execute implement: %w", err)
	}
	if !result.IsSuccess() {
		e.handlePhaseError(ctx, work, issueState, "implement", fmt.Errorf("agent failed: %s", result.Error))
		return fmt.Errorf("implement agent failed: %s", result.Error)
	}

	// Extract PR URL and build PR reference
	prURL := extractPRURL(result.Output)
	if prURL != "" {
		e.state.SetPRURL(issueState.ID, prURL)
	}
	prRef := extractPRRef(prURL)

	// Update progress
	progress := getProgress(issueState.PhaseData)
	if result.SessionID != "" {
		progress.SessionID = result.SessionID
	}
	progress.ImplementRef = prRef
	data, _ := json.Marshal(progress)
	e.state.SetPhaseData(issueState.ID, string(data))

	// Post implementation summary
	summary := fmt.Sprintf("## Implementation Complete\n\n%s", result.Output)
	if prURL != "" {
		summary += fmt.Sprintf("\n\n**PR**: %s", prURL)
	}
	e.github.PostComment(ctx, work.Project.Repo, work.Issue.Number, summary)
	e.state.RecordComment(issueState.ID, state.PhaseImplement, 0, result.Output, "dayshift")

	// Update issue status tracker
	e.github.UpdateIssueStatus(ctx, work.Project.Repo, work.Issue.Number, gh.StatusUpdate{
		ResearchLink: progress.ResearchURL,
		PlanLink:     progress.PlanURL,
		ImplementRef: prRef,
	})

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

// ensureIssueBranch creates or checks out a branch for the given issue.
// Pulls latest changes on the default branch before creating a new branch.
func ensureIssueBranch(ctx context.Context, workDir string, issueNumber int, title string) (string, error) {
	branchPrefix := fmt.Sprintf("dayshift/%d-", issueNumber)

	// Check if a branch already exists for this issue
	cmd := exec.CommandContext(ctx, "git", "branch", "--list", branchPrefix+"*")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err == nil {
		existing := strings.TrimSpace(string(output))
		if existing != "" {
			branch := strings.TrimSpace(strings.Split(existing, "\n")[0])
			branch = strings.TrimPrefix(branch, "* ")
			branch = strings.TrimSpace(branch)
			if err := gitCheckout(ctx, workDir, branch); err != nil {
				return "", fmt.Errorf("checkout existing branch %s: %w", branch, err)
			}
			return branch, nil
		}
	}

	// Pull latest on the default branch before creating a new branch
	defaultBranch := getDefaultBranch(ctx, workDir)
	gitCheckout(ctx, workDir, defaultBranch)
	gitPull(ctx, workDir)

	// Create a new branch from the now-updated default branch
	branchName := fmt.Sprintf("dayshift/%d-%s", issueNumber, slugify(title))
	if len(branchName) > 80 {
		branchName = branchName[:80]
	}

	cmd = exec.CommandContext(ctx, "git", "checkout", "-b", branchName, defaultBranch)
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("create branch %s: %s", branchName, strings.TrimSpace(string(out)))
	}

	return branchName, nil
}

// gitCheckout switches to the given branch.
func gitCheckout(ctx context.Context, workDir, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", branch)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// gitPull pulls latest changes on the current branch.
func gitPull(ctx context.Context, workDir string) {
	cmd := exec.CommandContext(ctx, "git", "pull", "--ff-only")
	cmd.Dir = workDir
	_ = cmd.Run()
}

// getDefaultBranch returns the default branch (main or master).
func getDefaultBranch(ctx context.Context, workDir string) string {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return "main"
	}
	// Output is like "origin/main" — strip the remote prefix
	branch := strings.TrimSpace(string(output))
	if parts := strings.SplitN(branch, "/", 2); len(parts) == 2 {
		return parts[1]
	}
	return "main"
}

// slugify converts a title to a branch-name-safe slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	// Replace non-alphanumeric with hyphens
	var result []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			result = append(result, c)
		} else if len(result) > 0 && result[len(result)-1] != '-' {
			result = append(result, '-')
		}
	}
	return strings.Trim(string(result), "-")
}
