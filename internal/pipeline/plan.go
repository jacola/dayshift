package pipeline

import (
"context"
"encoding/json"
"fmt"

"github.com/marcus/dayshift/internal/agents"
"github.com/marcus/dayshift/internal/comments"
gh "github.com/marcus/dayshift/internal/github"
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

## Human Answers to Previous Questions
The maintainer has answered the decision questions. Their selected options (marked [x]) are:

%s

IMPORTANT: Incorporate these decisions into the final plan. Avoid asking the same questions
again. You may ask NEW questions if genuinely new decisions emerged.`, humanAnswers)
}

prompt += `

## Instructions
1. Create a concrete, actionable implementation plan
2. Be specific about files to modify, approaches to take, and testing strategy
3. If you have questions or need decisions from the maintainer, present them with checkbox options
4. Even if one option seems obviously better, present it as a recommendation with alternatives

## CRITICAL: Output Requirements
- Output your COMPLETE plan directly as your response text
- Do NOT write the plan to plan.md or any other file — your text output IS the plan
- The full content of your response will be posted as a GitHub comment
- If you write the plan to a file instead of outputting it, it will be LOST

If you have questions, append them at the end in this EXACT format:

` + comments.MarkerQuestions + `
## Questions for Human Review

The following decisions need your input before implementation can proceed.
Check the boxes next to your preferred options.

### 1. [Decision title]
[Context and tradeoffs]
- [ ] Option A: [description] (recommended because...)
- [ ] Option B: [description]

### 2. [Next decision]
[Context]
- [ ] Option A
- [ ] Option B

` + comments.MarkerQuestionsEnd

return prompt
}

func (e *Executor) executePlan(ctx context.Context, work scanner.PendingWork, issueState *state.IssueState) error {
// Transition to plan phase
if issueState.Phase == state.PhaseResearch {
if err := e.state.TransitionPhase(issueState.ID, state.PhaseResearch, state.PhasePlan, "starting plan"); err != nil {
return fmt.Errorf("transition to plan: %w", err)
}
} else if issueState.Phase == state.PhaseClarify {
if err := e.state.TransitionPhase(issueState.ID, state.PhaseClarify, state.PhasePlan, "re-planning with answers"); err != nil {
return fmt.Errorf("transition from clarify to plan: %w", err)
}
}

research := e.getResearchFromComments(ctx, work)

existingPlan := e.getPlanFromComments(ctx, work)
if existingPlan == "(no plan found)" || existingPlan == "(plan not available)" {
existingPlan = ""
}

humanAnswers := ""
if work.Reason == "human_replied" || work.Reason == "questions_answered" {
humanAnswers = e.getAnsweredQuestions(ctx, work)
}

// Resume session from research phase
sessionID := getSessionID(issueState.PhaseData)

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

// Store updated session ID
progress := getProgress(issueState.PhaseData)
if result.SessionID != "" {
progress.SessionID = result.SessionID
}

// Post or update the plan comment (edit in place if one already exists)
commentBody := comments.WrapWithMarker(
comments.MarkerPlan, comments.MarkerPlanEnd,
cleanAgentOutput(result.Output),
)

var planURL string
existingPlanComment, _ := e.github.FindCommentByMarker(ctx, work.Project.Repo, work.Issue.Number, comments.MarkerPlan)
if existingPlanComment != nil && existingPlanComment.DatabaseID > 0 {
if err := e.github.EditComment(ctx, work.Project.Repo, existingPlanComment.DatabaseID, commentBody); err != nil {
return fmt.Errorf("edit plan comment: %w", err)
}
// Reconstruct comment URL from existing comment ID
planURL = existingPlanComment.ID
} else {
url, err := e.github.PostComment(ctx, work.Project.Repo, work.Issue.Number, commentBody)
if err != nil {
return fmt.Errorf("post plan comment: %w", err)
}
planURL = url
}

// Update progress with plan URL
progress.PlanURL = planURL
data, _ := json.Marshal(progress)
e.state.SetPhaseData(issueState.ID, string(data))

// Update issue status tracker
e.github.UpdateIssueStatus(ctx, work.Project.Repo, work.Issue.Number, gh.StatusUpdate{
ResearchLink: progress.ResearchURL,
PlanLink:     planURL,
})

e.state.RecordComment(issueState.ID, state.PhasePlan, 0, result.Output, "dayshift")

// Check for questions
hasQuestions := comments.HasMarker(result.Output, comments.MarkerQuestions)

if hasQuestions {
e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:needs-input")
if err := e.state.TransitionPhase(issueState.ID, state.PhasePlan, state.PhaseClarify, "questions for human"); err != nil {
return fmt.Errorf("transition to clarify: %w", err)
}
e.logger.Infof("plan for %s#%d has questions — waiting for human input", work.Project.Repo, work.Issue.Number)
} else {
e.github.AddLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:planned")
e.github.RemoveLabel(ctx, work.Project.Repo, work.Issue.Number, "dayshift:needs-input")
if err := e.state.TransitionPhase(issueState.ID, state.PhasePlan, state.PhaseImplement, "plan complete"); err != nil {
return fmt.Errorf("transition to implement: %w", err)
}
e.logger.Infof("plan complete for %s#%d — proceeding to implement", work.Project.Repo, work.Issue.Number)
}

return nil
}

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

func (e *Executor) getAnsweredQuestions(ctx context.Context, work scanner.PendingWork) string {
existing, err := e.github.FindCommentByMarker(ctx, work.Project.Repo, work.Issue.Number, comments.MarkerQuestions)
if err != nil || existing == nil {
return ""
}

questionsContent, found := comments.ExtractMarkedContent(existing.Body, comments.MarkerQuestions, comments.MarkerQuestionsEnd)
if !found {
return ""
}

return questionsContent
}
