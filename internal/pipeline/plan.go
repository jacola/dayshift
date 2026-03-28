package pipeline

import (
	"context"
	"fmt"

	"github.com/marcus/dayshift/internal/agents"
	"github.com/marcus/dayshift/internal/comments"
	"github.com/marcus/dayshift/internal/scanner"
	"github.com/marcus/dayshift/internal/state"
)

func buildPlanPrompt(issue scanner.PendingWork, research string, existingPlan string, humanAnswers string) string {
	prompt := fmt.Sprintf(`You are a planning agent. Create a detailed implementation plan for this GitHub issue.

## Issue
Title: %s
Number: #%d
Repository: %s

## Issue Body
%s

## Research
%s`,
		issue.Issue.Title,
		issue.Issue.Number,
		issue.Project.Repo,
		issue.Issue.Body,
		research,
	)

	if existingPlan != "" {
		prompt += fmt.Sprintf(`

## Existing Plan (update this, do not start from scratch)
%s`, existingPlan)
	}

	if humanAnswers != "" {
		prompt += fmt.Sprintf(`

## Human Answers to Research Questions
The maintainer has answered the decision questions from research. Their selected options (marked [x]) are:

%s`, humanAnswers)
	}

	prompt += `

## Instructions
1. Create a concrete, actionable implementation plan
2. Be specific about files to modify, approaches to take, and testing strategy
3. Incorporate the human's answers as firm decisions — do not second-guess them
4. For any remaining ambiguity, state your assumption clearly in an "Assumptions" section
5. Do NOT ask new questions — make reasonable assumptions and document them
6. The maintainer can comment on the issue if they disagree with any assumption

## CRITICAL: Output Requirements
- Output your COMPLETE plan directly as your response text
- Do NOT write the plan to plan.md or any other file — your text output IS the plan
- The full content of your response will be posted as a GitHub comment
- If you write the plan to a file instead of outputting it, it will be LOST
- Do NOT include a Questions section — all questions were resolved during research`

	return prompt
}

func (e *Executor) executePlan(ctx context.Context, work scanner.PendingWork, issueState *state.IssueState) error {
	// Transition to plan phase if coming from research
	if issueState.Phase == state.PhaseResearch {
		if err := e.state.TransitionPhase(issueState.ID, state.PhaseResearch, state.PhasePlan, "starting plan"); err != nil {
			return fmt.Errorf("transition to plan: %w", err)
		}
	} else if issueState.Phase == state.PhaseClarify {
		if err := e.state.TransitionPhase(issueState.ID, state.PhaseClarify, state.PhasePlan, "re-planning with answers"); err != nil {
			return fmt.Errorf("transition from clarify to plan: %w", err)
		}
	}

	// Gather research from issue comments
	research := e.getResearchFromComments(ctx, work)

	// Get existing plan if one exists (so we update rather than start from scratch)
	existingPlan := e.getPlanFromComments(ctx, work)
	if existingPlan == "(no plan found)" || existingPlan == "(plan not available)" {
		existingPlan = ""
	}

	// Gather human answers — get the existing plan comment with checked boxes
	humanAnswers := ""
	if work.Reason == "human_replied" || work.Reason == "questions_answered" {
		humanAnswers = e.getAnsweredQuestions(ctx, work)
	}

	// Try to resume the session from research phase
	sessionID := getSessionID(issueState.PhaseData)

	// Build and execute prompt
	prompt := buildPlanPrompt(work, research, existingPlan, humanAnswers)
	result, err := e.agent.Execute(ctx, agents.ExecuteOptions{
		Prompt:    prompt,
		WorkDir:   work.Project.Path,
		SessionID: sessionID,
	})
	if err != nil {
		e.handlePhaseError(ctx, work, issueState, "plan", err)
		return fmt.Errorf("execute plan: %w", err)
	}
	if !result.IsSuccess() {
		e.handlePhaseError(ctx, work, issueState, "plan", fmt.Errorf("agent failed: %s", result.Error))
		return fmt.Errorf("plan agent failed: %s", result.Error)
	}

	// Post or update the plan comment (edit in place if one already exists)
	commentBody := comments.WrapWithMarker(
		comments.MarkerPlan, comments.MarkerPlanEnd,
		cleanAgentOutput(result.Output),
	)

	existingPlanComment, _ := e.github.FindCommentByMarker(ctx, work.Project.Repo, work.Issue.Number, comments.MarkerPlan)
	if existingPlanComment != nil && existingPlanComment.DatabaseID > 0 {
		// Edit existing plan comment in place
		if err := e.github.EditComment(ctx, work.Project.Repo, existingPlanComment.DatabaseID, commentBody); err != nil {
			return fmt.Errorf("edit plan comment: %w", err)
		}
	} else {
		// First plan — post new comment
		if err := e.github.PostComment(ctx, work.Project.Repo, work.Issue.Number, commentBody); err != nil {
			return fmt.Errorf("post plan comment: %w", err)
		}
	}

	// Record comment and store plan data
	e.state.RecordComment(issueState.ID, state.PhasePlan, 0, result.Output, "dayshift")
	e.state.SetPhaseData(issueState.ID, result.Output)

	// Plan is complete — always move to approve (no questions from plan phase)
	e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:planned")
	e.github.RemoveLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:needs-input")
	if err := e.state.TransitionPhase(issueState.ID, state.PhasePlan, state.PhaseApprove, "plan complete"); err != nil {
		return fmt.Errorf("transition to approve: %w", err)
	}
	e.logger.Infof("plan complete for %s#%d — awaiting approval next run", work.Project.Repo, work.Issue.Number)

	return nil
}

// getResearchFromComments finds the research comment on the issue.
func (e *Executor) getResearchFromComments(ctx context.Context, work scanner.PendingWork) string {
	ghComments, err := e.github.GetComments(ctx, work.Project.Repo, work.Issue.Number)
	if err != nil {
		e.logger.Errorf("get comments for research: %v", err)
		return "(research not available)"
	}

	var bodies []string
	for _, c := range ghComments {
		bodies = append(bodies, c.Body)
	}

	body, found := comments.FindMarkedComment(bodies, comments.MarkerResearch)
	if !found {
		return "(no research found)"
	}

	content, _ := comments.ExtractMarkedContent(body, comments.MarkerResearch, comments.MarkerResearchEnd)
	return content
}

// getAnsweredQuestions extracts the questions section with checked answers from the existing plan comment.
func (e *Executor) getAnsweredQuestions(ctx context.Context, work scanner.PendingWork) string {
	existingPlan, err := e.github.FindCommentByMarker(ctx, work.Project.Repo, work.Issue.Number, comments.MarkerQuestions)
	if err != nil || existingPlan == nil {
		return ""
	}

	questionsContent, found := comments.ExtractMarkedContent(existingPlan.Body, comments.MarkerQuestions, comments.MarkerQuestionsEnd)
	if !found {
		return ""
	}

	return questionsContent
}
