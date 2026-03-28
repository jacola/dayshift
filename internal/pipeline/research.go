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

## CRITICAL: Output Requirements
- Output your COMPLETE research findings directly as your response text
- Do NOT write research to plan.md or any other file — your text output IS the research document
- Do NOT summarize — include ALL details, code snippets, file paths, and analysis
- The full content of your response will be posted as a GitHub comment
- If you write findings to a file instead of outputting them, they will be LOST

Format your output as a structured markdown document with clear sections.`,
issue.Issue.Title,
issue.Issue.Number,
issue.Project.Repo,
issue.Issue.Body,
)
}

func (e *Executor) executeResearch(ctx context.Context, work scanner.PendingWork, issueState *state.IssueState) error {
if issueState.Phase == state.PhasePending {
if err := e.state.TransitionPhase(issueState.ID, state.PhasePending, state.PhaseResearch, "starting research"); err != nil {
return fmt.Errorf("transition to research: %w", err)
}
}

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

output := cleanAgentOutput(result.Output)

// Store session ID for resuming in plan phase
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

e.state.RecordComment(issueState.ID, state.PhaseResearch, 0, output, "dayshift")
e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:researched")

// Always proceed to plan — questions happen there
if err := e.state.TransitionPhase(issueState.ID, state.PhaseResearch, state.PhasePlan, "research complete"); err != nil {
return fmt.Errorf("transition to plan: %w", err)
}

e.logger.Infof("research complete for %s#%d", work.Project.Repo, work.Issue.Number)
return nil
}
