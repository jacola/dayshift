package pipeline

import (
	"context"
	"fmt"

	"github.com/marcus/dayshift/internal/agents"
	"github.com/marcus/dayshift/internal/comments"
	"github.com/marcus/dayshift/internal/scanner"
	"github.com/marcus/dayshift/internal/state"
)

func buildResearchPrompt(issue scanner.PendingWork) string {
	return fmt.Sprintf(`You are a research agent. Analyze the codebase to understand the context needed for implementing this GitHub issue.

## Issue
Title: %s
Number: #%d
Repository: %s

## Issue Body
%s

## Instructions
1. Research the codebase thoroughly to understand relevant files, patterns, and constraints
2. Identify which parts of the codebase are affected by this issue
3. Note any related issues, PRs, or documentation
4. Look for existing patterns that should be followed
5. Include file paths with line references where relevant
6. Identify ALL implementation decisions and ambiguities that need human input

## Decision Questions
After your research, you MUST identify every decision point where:
- Multiple implementation approaches exist
- The issue description is ambiguous about scope or behavior
- There are tradeoffs the maintainer should weigh in on

For EACH decision, present options with pros/cons using checkboxes so the maintainer can select their preference.

## CRITICAL: Output Requirements
- Output your COMPLETE research findings directly as your response text
- Do NOT write research to plan.md or any other file — your text output IS the research document
- Do NOT summarize — include ALL details, code snippets, file paths, and analysis
- The full content of your response will be posted as a GitHub comment
- If you write findings to a file instead of outputting them, they will be LOST

Format your output as a structured markdown document. End with a Questions section in this EXACT format:

`+comments.MarkerQuestions+`
## Questions for Human Review

The following decisions need your input before we create an implementation plan.
Check the boxes next to your preferred options.

### 1. [Decision title]
[Context and tradeoffs]
- [ ] Option A: [description] (recommended because...)
- [ ] Option B: [description]

### 2. [Next decision]
[Context]
- [ ] Option A
- [ ] Option B

`+comments.MarkerQuestionsEnd,
		issue.Issue.Title,
		issue.Issue.Number,
		issue.Project.Repo,
		issue.Issue.Body,
	)
}

func (e *Executor) executeResearch(ctx context.Context, work scanner.PendingWork, issueState *state.IssueState) error {
	// Transition to research phase
	if issueState.Phase == state.PhasePending {
		if err := e.state.TransitionPhase(issueState.ID, state.PhasePending, state.PhaseResearch, "starting research"); err != nil {
			return fmt.Errorf("transition to research: %w", err)
		}
	}

	// Build and execute prompt
	prompt := buildResearchPrompt(work)
	result, err := e.agent.Execute(ctx, agents.ExecuteOptions{
		Prompt:  prompt,
		WorkDir: work.Project.Path,
	})
	if err != nil {
		e.handlePhaseError(ctx, work, issueState, "research", err)
		return fmt.Errorf("execute research: %w", err)
	}
	if !result.IsSuccess() {
		e.handlePhaseError(ctx, work, issueState, "research", fmt.Errorf("agent failed: %s", result.Error))
		return fmt.Errorf("research agent failed: %s", result.Error)
	}

	// Strip agent "thinking" preamble — keep only the research document.
	// Look for the first markdown heading or horizontal rule as the document start.
	output := cleanAgentOutput(result.Output)

	// Store session ID for resuming in later phases
	if result.SessionID != "" {
		phaseData := fmt.Sprintf(`{"session_id":"%s"}`, result.SessionID)
		e.state.SetPhaseData(issueState.ID, phaseData)
	}

	// Post research as comment
	commentBody := comments.WrapWithMarker(
		comments.MarkerResearch, comments.MarkerResearchEnd,
		output,
	)
	if err := e.github.PostComment(ctx, work.Project.Repo, work.Issue.Number, commentBody); err != nil {
		return fmt.Errorf("post research comment: %w", err)
	}

	// Record comment
	e.state.RecordComment(issueState.ID, state.PhaseResearch, 0, output, "dayshift")

	// Add researched label
	e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:researched")

	// Check if research includes questions for the human
	hasQuestions := comments.HasMarker(output, comments.MarkerQuestions)

	if hasQuestions {
		// Questions need human input before planning
		e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:needs-input")
		if err := e.state.TransitionPhase(issueState.ID, state.PhaseResearch, state.PhaseClarify, "research has questions"); err != nil {
			return fmt.Errorf("transition to clarify: %w", err)
		}
		e.logger.Infof("research complete for %s#%d — questions for human", work.Project.Repo, work.Issue.Number)
	} else {
		// No questions — proceed to plan
		if err := e.state.TransitionPhase(issueState.ID, state.PhaseResearch, state.PhasePlan, "research complete"); err != nil {
			return fmt.Errorf("transition to plan: %w", err)
		}
		e.logger.Infof("research complete for %s#%d", work.Project.Repo, work.Issue.Number)
	}

	return nil
}
