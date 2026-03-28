// Package scanner polls GitHub repos for issues needing dayshift processing.
package scanner

import (
	"context"
	"fmt"
	"sort"

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
// Returns a sorted list of work items (by project priority, then issue age).
func (s *Scanner) Scan(ctx context.Context) ([]PendingWork, error) {
	var work []PendingWork

	triggerLabel := s.config.Labels.Trigger
	if triggerLabel == "" {
		triggerLabel = "dayshift"
	}

	for _, project := range s.config.Projects {
		items, err := s.scanProject(ctx, project, triggerLabel)
		if err != nil {
			s.logger.Errorf("scan %s: %v", project.Repo, err)
			continue
		}
		work = append(work, items...)
	}

	// Sort by project priority (desc), then issue age (asc — oldest first)
	sort.Slice(work, func(i, j int) bool {
		if work[i].Project.Priority != work[j].Project.Priority {
			return work[i].Project.Priority > work[j].Project.Priority
		}
		return work[i].Issue.CreatedAt.Before(work[j].Issue.CreatedAt)
	})

	return work, nil
}

func (s *Scanner) scanProject(ctx context.Context, project config.ProjectConfig, triggerLabel string) ([]PendingWork, error) {
	issues, err := s.github.ListIssues(ctx, project.Repo, triggerLabel)
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

		pending := s.determineWork(ghIssue, localIssue, project)
		if pending != nil {
			work = append(work, *pending)
		}
	}

	return work, nil
}

func (s *Scanner) determineWork(ghIssue gh.Issue, localIssue *state.IssueState, project config.ProjectConfig) *PendingWork {
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

	case state.PhaseClarify:
		// Check if human has replied since our last comment
		latestComment, _ := s.state.GetLatestDayshiftComment(localIssue.ID)
		if latestComment != nil {
			humanComments, _ := s.state.GetHumanCommentsSince(localIssue.ID, latestComment.CreatedAt)
			if len(humanComments) > 0 {
				return &PendingWork{
					Issue:      ghIssue,
					Project:    project,
					IssueState: localIssue,
					NextPhase:  state.PhasePlan,
					Reason:     "human_replied",
				}
			}
		}

	case state.PhaseApprove:
		// Check if human added the approved label
		if ghIssue.HasLabel("dayshift:approved") {
			return &PendingWork{
				Issue:      ghIssue,
				Project:    project,
				IssueState: localIssue,
				NextPhase:  state.PhaseImplement,
				Reason:     "approved",
			}
		}

	case state.PhaseError:
		// Could be retried — but for now, skip
		return nil
	}

	return nil
}
