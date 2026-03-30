// Package state manages persistent issue state in SQLite.
package state

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/marcus/dayshift/internal/db"
)

// Valid phases in the issue pipeline.
const (
	PhasePending    = "pending"
	PhaseResearch   = "research"
	PhasePlan       = "plan"
	PhaseClarify    = "clarify"
	PhaseApprove    = "approve"
	PhaseImplement  = "implement"
	PhaseValidate   = "validate"
	PhaseComplete   = "complete"
	PhaseError      = "error"
	PhasePaused     = "paused"
)

// ValidTransitions defines which phase transitions are allowed.
var ValidTransitions = map[string][]string{
	PhasePending:   {PhaseResearch},
	PhaseResearch:  {PhasePlan, PhaseError},
	PhasePlan:      {PhaseClarify, PhaseImplement, PhaseError},
	PhaseClarify:   {PhasePlan, PhaseImplement, PhaseError},
	PhaseImplement: {PhaseValidate, PhaseError},
	PhaseValidate:  {PhaseComplete, PhaseImplement, PhaseError},
	PhaseError:     {PhasePending}, // Can restart from error
	PhasePaused:    {PhasePending, PhaseResearch, PhasePlan, PhaseClarify, PhaseImplement, PhaseValidate},
}

// IssueState represents the persisted state of an issue.
type IssueState struct {
	ID           int
	Repo         string
	IssueNumber  int
	Title        string
	Phase        string
	PhaseStarted *time.Time
	PhaseData    string
	PRURL        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CommentRecord represents a tracked comment.
type CommentRecord struct {
	ID        int
	IssueID   int
	Phase     string
	CommentID int
	Content   string
	Author    string
	CreatedAt time.Time
}

// RunRecord represents a processing run.
type RunRecord struct {
	ID              int
	StartTime       time.Time
	EndTime         *time.Time
	Provider        string
	Repo            string
	IssuesProcessed int
	Status          string
	Error           string
}

// Manager provides thread-safe access to issue state.
type Manager struct {
	db *db.DB
	mu sync.RWMutex
}

// New creates a new state Manager.
func New(database *db.DB) (*Manager, error) {
	if database == nil {
		return nil, fmt.Errorf("database is nil")
	}
	return &Manager{db: database}, nil
}

// GetIssue retrieves the state for a specific issue.
func (m *Manager) GetIssue(repo string, number int) (*IssueState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var issue IssueState
	var phaseStarted sql.NullTime
	err := m.db.SQL().QueryRow(
		`SELECT id, repo, issue_number, title, phase, phase_started, COALESCE(phase_data, ''), COALESCE(pr_url, ''), created_at, updated_at
		 FROM issues WHERE repo = ? AND issue_number = ?`,
		repo, number,
	).Scan(&issue.ID, &issue.Repo, &issue.IssueNumber, &issue.Title, &issue.Phase,
		&phaseStarted, &issue.PhaseData, &issue.PRURL, &issue.CreatedAt, &issue.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get issue %s#%d: %w", repo, number, err)
	}
	if phaseStarted.Valid {
		issue.PhaseStarted = &phaseStarted.Time
	}
	return &issue, nil
}

// UpsertIssue creates or updates an issue record.
func (m *Manager) UpsertIssue(issue *IssueState) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, err := m.db.SQL().Exec(
		`INSERT INTO issues (repo, issue_number, title, phase, phase_started, phase_data, pr_url, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(repo, issue_number) DO UPDATE SET
		   title = excluded.title,
		   phase = excluded.phase,
		   phase_started = excluded.phase_started,
		   phase_data = excluded.phase_data,
		   pr_url = excluded.pr_url,
		   updated_at = CURRENT_TIMESTAMP`,
		issue.Repo, issue.IssueNumber, issue.Title, issue.Phase, issue.PhaseStarted, issue.PhaseData, issue.PRURL,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert issue %s#%d: %w", issue.Repo, issue.IssueNumber, err)
	}

	id, _ := result.LastInsertId()
	return int(id), nil
}

// TransitionPhase moves an issue from one phase to another, recording history.
func (m *Manager) TransitionPhase(issueID int, from, to, reason string) error {
	if !isValidTransition(from, to) {
		return fmt.Errorf("invalid transition from %q to %q", from, to)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.SQL().Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Verify current phase matches expected
	var currentPhase string
	err = tx.QueryRow(`SELECT phase FROM issues WHERE id = ?`, issueID).Scan(&currentPhase)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("get current phase: %w", err)
	}
	if currentPhase != from {
		_ = tx.Rollback()
		return fmt.Errorf("phase mismatch: expected %q, got %q", from, currentPhase)
	}

	// Update phase
	now := time.Now()
	_, err = tx.Exec(
		`UPDATE issues SET phase = ?, phase_started = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		to, now, issueID,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("update phase: %w", err)
	}

	// Record history
	_, err = tx.Exec(
		`INSERT INTO phase_history (issue_id, from_phase, to_phase, reason) VALUES (?, ?, ?, ?)`,
		issueID, from, to, reason,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record history: %w", err)
	}

	return tx.Commit()
}

// SetPhaseData stores phase-specific JSON data for an issue.
func (m *Manager) SetPhaseData(issueID int, data string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.SQL().Exec(
		`UPDATE issues SET phase_data = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		data, issueID,
	)
	if err != nil {
		return fmt.Errorf("set phase data: %w", err)
	}
	return nil
}

// SetPRURL stores the PR URL for an issue.
func (m *Manager) SetPRURL(issueID int, url string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.SQL().Exec(
		`UPDATE issues SET pr_url = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		url, issueID,
	)
	if err != nil {
		return fmt.Errorf("set pr url: %w", err)
	}
	return nil
}

// RecordComment stores a comment record for tracking.
func (m *Manager) RecordComment(issueID int, phase string, commentID int, content, author string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.SQL().Exec(
		`INSERT INTO issue_comments (issue_id, phase, comment_id, content, author) VALUES (?, ?, ?, ?, ?)`,
		issueID, phase, commentID, content, author,
	)
	if err != nil {
		return fmt.Errorf("record comment: %w", err)
	}
	return nil
}

// GetLatestDayshiftComment returns the most recent comment by dayshift on an issue.
func (m *Manager) GetLatestDayshiftComment(issueID int) (*CommentRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var c CommentRecord
	err := m.db.SQL().QueryRow(
		`SELECT id, issue_id, phase, comment_id, content, author, created_at
		 FROM issue_comments WHERE issue_id = ? AND author = 'dayshift'
		 ORDER BY created_at DESC LIMIT 1`,
		issueID,
	).Scan(&c.ID, &c.IssueID, &c.Phase, &c.CommentID, &c.Content, &c.Author, &c.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest dayshift comment: %w", err)
	}
	return &c, nil
}

// GetHumanCommentsSince returns non-dayshift comments posted after a given time.
func (m *Manager) GetHumanCommentsSince(issueID int, since time.Time) ([]CommentRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.SQL().Query(
		`SELECT id, issue_id, phase, comment_id, content, author, created_at
		 FROM issue_comments WHERE issue_id = ? AND author != 'dayshift' AND created_at > ?
		 ORDER BY created_at ASC`,
		issueID, since.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil, fmt.Errorf("get human comments: %w", err)
	}
	defer rows.Close()

	var comments []CommentRecord
	for rows.Next() {
		var c CommentRecord
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Phase, &c.CommentID, &c.Content, &c.Author, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// ListIssuesByPhase returns all issues in a given phase.
func (m *Manager) ListIssuesByPhase(phase string) ([]IssueState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.SQL().Query(
		`SELECT id, repo, issue_number, title, phase, phase_started, COALESCE(phase_data, ''), COALESCE(pr_url, ''), created_at, updated_at
		 FROM issues WHERE phase = ? ORDER BY updated_at ASC`,
		phase,
	)
	if err != nil {
		return nil, fmt.Errorf("list issues by phase: %w", err)
	}
	defer rows.Close()

	var issues []IssueState
	for rows.Next() {
		var issue IssueState
		var phaseStarted sql.NullTime
		if err := rows.Scan(&issue.ID, &issue.Repo, &issue.IssueNumber, &issue.Title, &issue.Phase,
			&phaseStarted, &issue.PhaseData, &issue.PRURL, &issue.CreatedAt, &issue.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan issue: %w", err)
		}
		if phaseStarted.Valid {
			issue.PhaseStarted = &phaseStarted.Time
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// ListAllIssues returns all tracked issues.
func (m *Manager) ListAllIssues() ([]IssueState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.SQL().Query(
		`SELECT id, repo, issue_number, title, phase, phase_started, COALESCE(phase_data, ''), COALESCE(pr_url, ''), created_at, updated_at
		 FROM issues ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all issues: %w", err)
	}
	defer rows.Close()

	var issues []IssueState
	for rows.Next() {
		var issue IssueState
		var phaseStarted sql.NullTime
		if err := rows.Scan(&issue.ID, &issue.Repo, &issue.IssueNumber, &issue.Title, &issue.Phase,
			&phaseStarted, &issue.PhaseData, &issue.PRURL, &issue.CreatedAt, &issue.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan issue: %w", err)
		}
		if phaseStarted.Valid {
			issue.PhaseStarted = &phaseStarted.Time
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// RecordRun records a processing run.
func (m *Manager) RecordRun(run *RunRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.SQL().Exec(
		`INSERT INTO run_history (start_time, end_time, provider, repo, issues_processed, status, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.StartTime, run.EndTime, run.Provider, run.Repo, run.IssuesProcessed, run.Status, run.Error,
	)
	if err != nil {
		return fmt.Errorf("record run: %w", err)
	}
	return nil
}

// GetRecentRuns returns the last N runs.
func (m *Manager) GetRecentRuns(n int) ([]RunRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.SQL().Query(
		`SELECT id, start_time, end_time, provider, repo, issues_processed, status, COALESCE(error, '')
		 FROM run_history ORDER BY start_time DESC LIMIT ?`, n,
	)
	if err != nil {
		return nil, fmt.Errorf("get recent runs: %w", err)
	}
	defer rows.Close()

	var runs []RunRecord
	for rows.Next() {
		var r RunRecord
		var endTime sql.NullTime
		if err := rows.Scan(&r.ID, &r.StartTime, &endTime, &r.Provider, &r.Repo, &r.IssuesProcessed, &r.Status, &r.Error); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		if endTime.Valid {
			r.EndTime = &endTime.Time
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// isValidTransition checks if a phase transition is allowed.
func isValidTransition(from, to string) bool {
	// Any phase can transition to paused
	if to == PhasePaused {
		return true
	}
	allowed, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == to {
			return true
		}
	}
	return false
}
