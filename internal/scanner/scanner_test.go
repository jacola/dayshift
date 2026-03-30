package scanner

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcus/dayshift/internal/config"
	"github.com/marcus/dayshift/internal/db"
	gh "github.com/marcus/dayshift/internal/github"
	"github.com/marcus/dayshift/internal/state"
)

func setupTest(t *testing.T) (*state.Manager, *db.DB) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	mgr, err := state.New(database)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	return mgr, database
}

func TestScanNewIssue(t *testing.T) {
	mgr, _ := setupTest(t)
	cfg := &config.Config{
		Labels: config.LabelsConfig{Trigger: "dayshift"},
		Projects: []config.ProjectConfig{
			{Repo: "owner/repo", Path: "/tmp/repo", Priority: 1},
		},
	}

	// No local state for this issue — it should be detected as new
	scanner := New(nil, mgr, cfg)
	issue := gh.Issue{
		Number:    1,
		Title:     "New feature",
		CreatedAt: time.Now(),
	}

	pending := scanner.determineWork(context.Background(), issue, nil, cfg.Projects[0])
	if pending == nil {
		t.Fatal("expected pending work for new issue")
	}
	if pending.NextPhase != state.PhaseResearch {
		t.Errorf("expected next phase %s, got %s", state.PhaseResearch, pending.NextPhase)
	}
	if pending.Reason != "new_issue" {
		t.Errorf("expected reason new_issue, got %s", pending.Reason)
	}
}

func TestScanPendingIssue(t *testing.T) {
	mgr, _ := setupTest(t)
	cfg := &config.Config{
		Projects: []config.ProjectConfig{
			{Repo: "owner/repo", Path: "/tmp/repo"},
		},
	}

	mgr.UpsertIssue(&state.IssueState{
		Repo: "owner/repo", IssueNumber: 1, Title: "Test", Phase: state.PhasePending,
	})
	localIssue, _ := mgr.GetIssue("owner/repo", 1)

	scanner := New(nil, mgr, cfg)
	issue := gh.Issue{Number: 1, Title: "Test"}

	pending := scanner.determineWork(context.Background(), issue, localIssue, cfg.Projects[0])
	if pending == nil {
		t.Fatal("expected pending work")
	}
	if pending.NextPhase != state.PhaseResearch {
		t.Errorf("expected research, got %s", pending.NextPhase)
	}
}

func TestScanApprovedIssue(t *testing.T) {
	mgr, _ := setupTest(t)
	cfg := &config.Config{
		Projects: []config.ProjectConfig{
			{Repo: "owner/repo", Path: "/tmp/repo"},
		},
	}

	// Issues in the plan phase with no questions go straight to implement
	mgr.UpsertIssue(&state.IssueState{
		Repo: "owner/repo", IssueNumber: 1, Title: "Test", Phase: state.PhaseImplement,
	})
	localIssue, _ := mgr.GetIssue("owner/repo", 1)

	scanner := New(nil, mgr, cfg)

	issue := gh.Issue{
		Number: 1,
		Title:  "Test",
		Labels: []gh.GHLabel{{Name: "dayshift"}, {Name: "dayshift:planned"}},
	}

	pending := scanner.determineWork(context.Background(), issue, localIssue, cfg.Projects[0])
	if pending == nil {
		t.Fatal("expected pending work for implement phase issue")
	}
	if pending.NextPhase != state.PhaseValidate {
		t.Errorf("expected validate, got %s", pending.NextPhase)
	}
}

func TestScanSkipsPaused(t *testing.T) {
	mgr, _ := setupTest(t)
	cfg := &config.Config{
		Labels: config.LabelsConfig{Trigger: "dayshift"},
		Projects: []config.ProjectConfig{
			{Repo: "owner/repo", Path: "/tmp/repo"},
		},
	}

	scanner := New(nil, mgr, cfg)

	// Paused issue should return nil
	issue := gh.Issue{
		Number: 1,
		Labels: []gh.GHLabel{{Name: "dayshift"}, {Name: "dayshift:paused"}},
	}

	// scanProject filters paused, but we test determineWork with a local issue
	localIssue := &state.IssueState{Phase: state.PhasePaused}
	pending := scanner.determineWork(context.Background(), issue, localIssue, cfg.Projects[0])
	if pending != nil {
		t.Error("expected no work for paused issue")
	}
}
