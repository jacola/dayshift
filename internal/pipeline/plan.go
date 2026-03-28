package pipeline

import (
	"context"
	"fmt"

	"github.com/marcus/dayshift/internal/agents"
	"github.com/marcus/dayshift/internal/comments"
	"github.com/marcus/dayshift/internal/scanner"
	"github.com/marcus/dayshift/internal/state"
)

func buildPlanPrompt(issue scanner.PendingWork, research string, humanAnswers string) string {
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

	if humanAnswers != "" {
		prompt += fmt.Sprintf(`

## Human Answers to Previous Questions
%s`, humanAnswers)
	}

	prompt += `

## Instructions
1. Create a concrete, actionable implementation plan
2. Be specific about files to modify, approaches to take, and testing strategy
3. If you have questions or need decisions from the maintainer, list them in a structured "Questions" section using the EXACT format below

## CRITICAL: Output Requirements
- Output your COMPLETE plan directly as your response text
- Do NOT write the plan to plan.md or any other file — your text output IS the plan
- The full content of your response will be posted as a GitHub comment
- If you write the plan to a file instead of outputting it, it will be LOST

If you have questions, append them in this EXACT format at the end:

` + comments.MarkerQuestions + `
## Questions for Human Review

The following decisions need your input before implementation can proceed.
Reply in a comment with your answers (reference by number).

### 1. [Question title]
[Question details with options if applicable]

### 2. [Next question]
[Details]

` + comments.MarkerQuestionsEnd

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

	// Gather human answers if re-planning
	humanAnswers := ""
	if work.Reason == "human_replied" {
		humanAnswers = e.getHumanAnswers(ctx, work, issueState)
	}

	// Build and execute prompt
	prompt := buildPlanPrompt(work, research, humanAnswers)
	result, err := e.agent.Execute(ctx, agents.ExecuteOptions{
		Prompt:  prompt,
		WorkDir: work.Project.Path,
	})
	if err != nil {
		e.handlePhaseError(ctx, work, issueState, "plan", err)
		return fmt.Errorf("execute plan: %w", err)
	}
	if !result.IsSuccess() {
		e.handlePhaseError(ctx, work, issueState, "plan", fmt.Errorf("agent failed: %s", result.Error))
		return fmt.Errorf("plan agent failed: %s", result.Error)
	}

	// Post plan as comment
	commentBody := comments.WrapWithMarker(
		comments.MarkerPlan, comments.MarkerPlanEnd,
		result.Output,
	)
	if err := e.github.PostComment(ctx, work.Project.Repo, work.Issue.Number, commentBody); err != nil {
		return fmt.Errorf("post plan comment: %w", err)
	}

	// Record comment and store plan data
	e.state.RecordComment(issueState.ID, state.PhasePlan, 0, result.Output, "dayshift")
	e.state.SetPhaseData(issueState.ID, result.Output)

	// Check for questions
	hasQuestions := comments.HasMarker(result.Output, comments.MarkerQuestions)

	e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:planned")

	if hasQuestions {
		// Transition to clarify
		e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:needs-input")
		if err := e.state.TransitionPhase(issueState.ID, state.PhasePlan, state.PhaseClarify, "questions for human"); err != nil {
			return fmt.Errorf("transition to clarify: %w", err)
		}
		e.logger.Infof("plan for %s#%d has questions — waiting for human input", work.Project.Repo, work.Issue.Number)
	} else {
		// Transition to approve — next run will execute the approve phase
		if err := e.state.TransitionPhase(issueState.ID, state.PhasePlan, state.PhaseApprove, "plan complete"); err != nil {
			return fmt.Errorf("transition to approve: %w", err)
		}
		e.logger.Infof("plan complete for %s#%d — awaiting approval next run", work.Project.Repo, work.Issue.Number)
	}

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

// getHumanAnswers collects human replies since the last dayshift comment.
func (e *Executor) getHumanAnswers(ctx context.Context, work scanner.PendingWork, issueState *state.IssueState) string {
	latestComment, err := e.state.GetLatestDayshiftComment(issueState.ID)
	if err != nil || latestComment == nil {
		return ""
	}

	humanComments, err := e.state.GetHumanCommentsSince(issueState.ID, latestComment.CreatedAt)
	if err != nil {
		return ""
	}

	var answers string
	for _, c := range humanComments {
		answers += fmt.Sprintf("### Reply by %s\n%s\n\n", c.Author, c.Content)
	}
	return answers
}
