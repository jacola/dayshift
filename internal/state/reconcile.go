package state

import (
	"context"
	"fmt"

	"github.com/marcus/dayshift/internal/config"
	gh "github.com/marcus/dayshift/internal/github"
	"github.com/marcus/dayshift/internal/logging"
)

// labelToPhase maps dayshift labels to their corresponding phase.
var labelToPhase = map[string]string{
	"dayshift:researched":        PhaseResearch,
	"dayshift:planned":           PhasePlan,
	"dayshift:needs-input":       PhaseClarify,
	"dayshift:awaiting-approval": PhaseApprove,
	"dayshift:approved":          PhaseImplement,
	"dayshift:implementing":      PhaseImplement,
	"dayshift:implemented":       PhaseValidate,
	"dayshift:validated":         PhaseComplete,
	"dayshift:complete":          PhaseComplete,
	"dayshift:error":             PhaseError,
	"dayshift:paused":            PhasePaused,
}

// Reconcile synchronizes local SQLite state with GitHub label state.
// GitHub labels are the source of truth for human-initiated actions.
func (m *Manager) Reconcile(ctx context.Context, client *gh.Client, projects []config.ProjectConfig, triggerLabel string) error {
	logger := logging.Component("reconcile")

	for _, project := range projects {
		issues, err := client.ListIssues(ctx, project.Repo, triggerLabel, "")
		if err != nil {
			logger.Errorf("reconcile %s: %v", project.Repo, err)
			continue
		}

		for _, ghIssue := range issues {
			localIssue, err := m.GetIssue(project.Repo, ghIssue.Number)
			if err != nil {
				logger.Errorf("get issue %s#%d: %v", project.Repo, ghIssue.Number, err)
				continue
			}

			// Determine the most advanced phase indicated by GitHub labels
			githubPhase := determinePhaseFromLabels(ghIssue.LabelNames())

			if localIssue == nil {
				// New issue not yet tracked — create it
				_, err := m.UpsertIssue(&IssueState{
					Repo:        project.Repo,
					IssueNumber: ghIssue.Number,
					Title:       ghIssue.Title,
					Phase:       PhasePending,
				})
				if err != nil {
					logger.Errorf("create issue %s#%d: %v", project.Repo, ghIssue.Number, err)
				} else {
					logger.Infof("tracked new issue %s#%d", project.Repo, ghIssue.Number)
				}
				continue
			}

			// Check for human-initiated state changes
			if githubPhase != "" && githubPhase != localIssue.Phase {
				// GitHub labels indicate a different phase than our local state
				// Trust GitHub for human-initiated actions (approved, paused, etc.)
				if isHumanInitiated(githubPhase) {
					logger.Infof("reconcile %s#%d: %s → %s (from GitHub labels)",
						project.Repo, ghIssue.Number, localIssue.Phase, githubPhase)

					err := m.forcePhase(localIssue.ID, localIssue.Phase, githubPhase, "reconciled from GitHub labels")
					if err != nil {
						logger.Errorf("force phase %s#%d: %v", project.Repo, ghIssue.Number, err)
					}
				}
			}

			// Update title if changed
			if localIssue.Title != ghIssue.Title {
				localIssue.Title = ghIssue.Title
				_, _ = m.UpsertIssue(localIssue)
			}
		}
	}

	return nil
}

// determinePhaseFromLabels finds the most relevant phase from a set of labels.
func determinePhaseFromLabels(labels []string) string {
	// Priority order: later phases take precedence
	phaseOrder := []string{
		PhasePaused, PhaseError, PhaseComplete, PhaseValidate,
		PhaseImplement, PhaseApprove, PhaseClarify, PhasePlan, PhaseResearch,
	}

	labelPhases := make(map[string]bool)
	for _, label := range labels {
		if phase, ok := labelToPhase[label]; ok {
			labelPhases[phase] = true
		}
	}

	for _, phase := range phaseOrder {
		if labelPhases[phase] {
			return phase
		}
	}

	return ""
}

// isHumanInitiated returns true if the phase is typically set by humans.
func isHumanInitiated(phase string) bool {
	switch phase {
	case PhaseImplement: // from dayshift:approved label
		return true
	case PhasePaused:
		return true
	case PhasePending: // from removing all labels (restart)
		return true
	default:
		return false
	}
}

// forcePhase bypasses normal transition validation for reconciliation.
func (m *Manager) forcePhase(issueID int, from, to, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.SQL().Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE issues SET phase = ?, phase_started = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		to, issueID,
	)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	_, err = tx.Exec(
		`INSERT INTO phase_history (issue_id, from_phase, to_phase, reason) VALUES (?, ?, ?, ?)`,
		issueID, from, to, reason,
	)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
