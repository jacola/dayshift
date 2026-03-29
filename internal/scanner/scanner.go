// Package scanner polls GitHub repos for issues needing dayshift processing.
package scanner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/marcus/dayshift/internal/comments"
	"github.com/marcus/dayshift/internal/config"
	gh "github.com/marcus/dayshift/internal/github"
	"github.com/marcus/dayshift/internal/logging"
	"github.com/marcus/dayshift/internal/state"
)

// PendingWork represents an issue that needs processing.
type PendingWork struct {
	Issue      gh.Issue
	Project    config.ProjectConfig
	IssueState *state.IssueState
	NextPhase  string
	Reason     string
}

// Scanner polls repositories for issues needing processing.
type Scanner struct {
	github *gh.Client
	state  *state.Manager
	config *config.Config
	logger *logging.Logger
}

// New creates a new Scanner.
func New(client *gh.Client, mgr *state.Manager, cfg *config.Config) *Scanner {
	return &Scanner{
		github: client,
		state:  mgr,
		config: cfg,
		logger: logging.Component("scanner"),
	}
}

// Scan checks all configured projects for issues needing processing.
func (s *Scanner) Scan(ctx context.Context) ([]PendingWork, error) {
	var work []PendingWork

	triggerLabel := s.config.Labels.Trigger
	if triggerLabel == "" {
		triggerLabel = "dayshift"
	}

	// Resolve author filter
	author := s.resolveAuthorFilter(ctx)

	for _, project := range s.config.Projects {
		items, err := s.scanProject(ctx, project, triggerLabel, author)
		if err != nil {
			s.logger.Errorf("scan %s: %v", project.Repo, err)
			continue
		}
		work = append(work, items...)
	}

	sort.Slice(work, func(i, j int) bool {
		if work[i].Project.Priority != work[j].Project.Priority {
			return work[i].Project.Priority > work[j].Project.Priority
		}
		return work[i].Issue.CreatedAt.Before(work[j].Issue.CreatedAt)
	})

	return work, nil
}

// resolveAuthorFilter returns the GitHub username to filter by, or empty for all.
func (s *Scanner) resolveAuthorFilter(ctx context.Context) string {
	filter := s.config.Issues.AuthorFilter
	if filter == "" {
		filter = "self"
	}

	switch filter {
	case "all":
		return ""
	case "self":
		user, err := s.github.CurrentUser(ctx)
		if err != nil {
			s.logger.Warnf("could not resolve current user for author filter: %v", err)
			return ""
		}
		s.logger.Debugf("author filter: self → %s", user)
		return user
	default:
		return filter
	}
}

func (s *Scanner) scanProject(ctx context.Context, project config.ProjectConfig, triggerLabel string, author string) ([]PendingWork, error) {
	issues, err := s.github.ListIssues(ctx, project.Repo, triggerLabel, author)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	var work []PendingWork

	for _, ghIssue := range issues {
		// Skip paused issues
		if ghIssue.HasLabel("dayshift:paused") {
			continue
		}
		// Skip completed issues
		if ghIssue.HasLabel("dayshift:complete") {
			continue
		}

		localIssue, err := s.state.GetIssue(project.Repo, ghIssue.Number)
		if err != nil {
			s.logger.Errorf("get state for %s#%d: %v", project.Repo, ghIssue.Number, err)
			continue
		}

		pending := s.determineWork(ctx, ghIssue, localIssue, project)
		if pending != nil {
			work = append(work, *pending)
		}
	}

	return work, nil
}

func (s *Scanner) determineWork(ctx context.Context, ghIssue gh.Issue, localIssue *state.IssueState, project config.ProjectConfig) *PendingWork {
	// New issue — not yet tracked
	if localIssue == nil {
		return &PendingWork{
			Issue:     ghIssue,
			Project:   project,
			NextPhase: state.PhaseResearch,
			Reason:    "new_issue",
		}
	}

	switch localIssue.Phase {
	case state.PhasePending:
		return &PendingWork{
			Issue:      ghIssue,
			Project:    project,
			IssueState: localIssue,
			NextPhase:  state.PhaseResearch,
			Reason:     "pending_issue",
		}

	case state.PhasePlan:
		// Previous phase (research) completed and transitioned here — run plan
		return &PendingWork{
			Issue:      ghIssue,
			Project:    project,
			IssueState: localIssue,
			NextPhase:  state.PhasePlan,
			Reason:     "research_complete",
		}

	case state.PhaseClarify:
		// Check GitHub for human replies — either a new comment or checked boxes
		// Questions come from plan phase, so clarify always routes back to plan
		if s.github != nil {
			ghComments, err := s.github.GetComments(ctx, project.Repo, ghIssue.Number)
			if err == nil && len(ghComments) > 0 {
				// Find the last comment with questions marker
				lastQuestionsIdx := -1
				for i, c := range ghComments {
					if comments.HasMarker(c.Body, comments.MarkerQuestions) {
						lastQuestionsIdx = i
					}
				}

				if lastQuestionsIdx < 0 {
					break
				}

				// Check 1: New comment after the questions comment
				if lastQuestionsIdx < len(ghComments)-1 {
					return &PendingWork{
						Issue:      ghIssue,
						Project:    project,
						IssueState: localIssue,
						NextPhase:  state.PhasePlan,
						Reason:     "human_replied",
					}
				}

				// Check 2: Human has selected options (at least one [x] in questions section)
				body := ghComments[lastQuestionsIdx].Body
				questionsSection, found := comments.ExtractMarkedContent(body, comments.MarkerQuestions, comments.MarkerQuestionsEnd)
				if found && strings.Contains(questionsSection, "- [x]") {
					return &PendingWork{
						Issue:      ghIssue,
						Project:    project,
						IssueState: localIssue,
						NextPhase:  state.PhasePlan,
						Reason:     "questions_answered",
					}
				}
			}
		}

	case state.PhaseApprove:
		// Check if human added the approved label → proceed to implement
		if ghIssue.HasLabel("dayshift:approved") {
			return &PendingWork{
				Issue:      ghIssue,
				Project:    project,
				IssueState: localIssue,
				NextPhase:  state.PhaseImplement,
				Reason:     "approved",
			}
		}
		// If no awaiting-approval label yet, need to post the approval request
		if !ghIssue.HasLabel("dayshift:awaiting-approval") {
			return &PendingWork{
				Issue:      ghIssue,
				Project:    project,
				IssueState: localIssue,
				NextPhase:  state.PhaseApprove,
				Reason:     "needs_approval_request",
			}
		}

	case state.PhaseImplement:
		// Implementation was completed and transitioned here — run validate
		return &PendingWork{
			Issue:      ghIssue,
			Project:    project,
			IssueState: localIssue,
			NextPhase:  state.PhaseValidate,
			Reason:     "implementation_complete",
		}

	case state.PhaseValidate:
		// Validate phase ready to run
		return &PendingWork{
			Issue:      ghIssue,
			Project:    project,
			IssueState: localIssue,
			NextPhase:  state.PhaseValidate,
			Reason:     "ready_to_validate",
		}

	case state.PhaseError:
		// Could be retried — but for now, skip
		return nil
	}

	return nil
}
