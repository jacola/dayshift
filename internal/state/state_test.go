package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcus/dayshift/internal/db"
)

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	tmpDir := t.TempDir()
	database, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestNewManager(t *testing.T) {
	database := setupTestDB(t)
	mgr, err := New(database)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestNewManagerNilDB(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Error("expected error for nil db")
	}
}

func TestUpsertAndGetIssue(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	issue := &IssueState{
		Repo:        "owner/repo",
		IssueNumber: 42,
		Title:       "Test issue",
		Phase:       PhasePending,
	}

	id, err := mgr.UpsertIssue(issue)
	if err != nil {
		t.Fatalf("UpsertIssue: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	got, err := mgr.GetIssue("owner/repo", 42)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil issue")
	}
	if got.Title != "Test issue" || got.Phase != PhasePending {
		t.Errorf("unexpected: title=%q phase=%q", got.Title, got.Phase)
	}
}

func TestGetIssueNotFound(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	got, err := mgr.GetIssue("owner/repo", 999)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent issue")
	}
}

func TestUpsertUpdatesExisting(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	issue := &IssueState{Repo: "owner/repo", IssueNumber: 1, Title: "Original", Phase: PhasePending}
	mgr.UpsertIssue(issue)

	issue.Title = "Updated"
	issue.Phase = PhaseResearch
	mgr.UpsertIssue(issue)

	got, _ := mgr.GetIssue("owner/repo", 1)
	if got.Title != "Updated" || got.Phase != PhaseResearch {
		t.Errorf("expected updated values, got title=%q phase=%q", got.Title, got.Phase)
	}
}

func TestTransitionPhase(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	issue := &IssueState{Repo: "owner/repo", IssueNumber: 1, Title: "Test", Phase: PhasePending}
	id, _ := mgr.UpsertIssue(issue)

	err := mgr.TransitionPhase(id, PhasePending, PhaseResearch, "starting research")
	if err != nil {
		t.Fatalf("TransitionPhase: %v", err)
	}

	got, _ := mgr.GetIssue("owner/repo", 1)
	if got.Phase != PhaseResearch {
		t.Errorf("expected phase=%s, got %s", PhaseResearch, got.Phase)
	}
}

func TestTransitionPhaseInvalid(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	issue := &IssueState{Repo: "owner/repo", IssueNumber: 1, Title: "Test", Phase: PhasePending}
	id, _ := mgr.UpsertIssue(issue)

	// Can't go from pending to implement
	err := mgr.TransitionPhase(id, PhasePending, PhaseImplement, "skip ahead")
	if err == nil {
		t.Error("expected error for invalid transition")
	}
}

func TestTransitionPhaseMismatch(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	issue := &IssueState{Repo: "owner/repo", IssueNumber: 1, Title: "Test", Phase: PhasePending}
	id, _ := mgr.UpsertIssue(issue)

	// Claim it's in research when it's actually pending
	err := mgr.TransitionPhase(id, PhaseResearch, PhasePlan, "wrong from phase")
	if err == nil {
		t.Error("expected error for phase mismatch")
	}
}

func TestTransitionToPaused(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	issue := &IssueState{Repo: "owner/repo", IssueNumber: 1, Title: "Test", Phase: PhaseResearch}
	id, _ := mgr.UpsertIssue(issue)

	// Any phase can transition to paused
	err := mgr.TransitionPhase(id, PhaseResearch, PhasePaused, "user paused")
	if err != nil {
		t.Fatalf("TransitionPhase to paused: %v", err)
	}
}

func TestRecordAndGetComments(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	issue := &IssueState{Repo: "owner/repo", IssueNumber: 1, Title: "Test", Phase: PhasePending}
	id, _ := mgr.UpsertIssue(issue)

	err := mgr.RecordComment(id, PhaseResearch, 100, "Research findings", "dayshift")
	if err != nil {
		t.Fatalf("RecordComment: %v", err)
	}

	latest, err := mgr.GetLatestDayshiftComment(id)
	if err != nil {
		t.Fatalf("GetLatestDayshiftComment: %v", err)
	}
	if latest == nil {
		t.Fatal("expected non-nil comment")
	}
	if latest.Content != "Research findings" || latest.Author != "dayshift" {
		t.Errorf("unexpected comment: content=%q author=%q", latest.Content, latest.Author)
	}
}

func TestGetHumanCommentsSince(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	issue := &IssueState{Repo: "owner/repo", IssueNumber: 1, Title: "Test", Phase: PhasePending}
	id, _ := mgr.UpsertIssue(issue)

	mgr.RecordComment(id, PhasePlan, 100, "Agent question", "dayshift")
	mgr.RecordComment(id, PhasePlan, 101, "Human answer", "someuser")

	since := time.Now().Add(-1 * time.Hour)
	comments, err := mgr.GetHumanCommentsSince(id, since)
	if err != nil {
		t.Fatalf("GetHumanCommentsSince: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 human comment, got %d", len(comments))
	}
	if comments[0].Author != "someuser" {
		t.Errorf("expected author=someuser, got %s", comments[0].Author)
	}
}

func TestListIssuesByPhase(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	mgr.UpsertIssue(&IssueState{Repo: "owner/repo", IssueNumber: 1, Title: "A", Phase: PhasePending})
	mgr.UpsertIssue(&IssueState{Repo: "owner/repo", IssueNumber: 2, Title: "B", Phase: PhaseResearch})
	mgr.UpsertIssue(&IssueState{Repo: "owner/repo", IssueNumber: 3, Title: "C", Phase: PhasePending})

	pending, err := mgr.ListIssuesByPhase(PhasePending)
	if err != nil {
		t.Fatalf("ListIssuesByPhase: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending issues, got %d", len(pending))
	}
}

func TestRecordAndGetRuns(t *testing.T) {
	database := setupTestDB(t)
	mgr, _ := New(database)

	now := time.Now()
	end := now.Add(5 * time.Minute)
	err := mgr.RecordRun(&RunRecord{
		StartTime:       now,
		EndTime:         &end,
		Provider:        "claude",
		Repo:            "owner/repo",
		IssuesProcessed: 3,
		Status:          "success",
	})
	if err != nil {
		t.Fatalf("RecordRun: %v", err)
	}

	runs, err := mgr.GetRecentRuns(10)
	if err != nil {
		t.Fatalf("GetRecentRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Provider != "claude" || runs[0].IssuesProcessed != 3 {
		t.Errorf("unexpected run: provider=%s issues=%d", runs[0].Provider, runs[0].IssuesProcessed)
	}
}

func TestIsValidTransition(t *testing.T) {
	tests := []struct {
		from, to string
		valid    bool
	}{
		{PhasePending, PhaseResearch, true},
		{PhaseResearch, PhasePlan, true},
		{PhasePlan, PhaseClarify, true},
		{PhasePlan, PhaseImplement, true},
		{PhaseClarify, PhasePlan, true},
		{PhaseClarify, PhaseImplement, true},
		{PhaseImplement, PhaseValidate, true},
		{PhaseValidate, PhaseComplete, true},
		{PhaseValidate, PhaseImplement, true},
		{PhasePending, PhaseImplement, false},
		{PhaseResearch, PhaseComplete, false},
		{PhasePlan, PhaseValidate, false},
		// Any phase can go to paused
		{PhasePending, PhasePaused, true},
		{PhaseResearch, PhasePaused, true},
		{PhaseImplement, PhasePaused, true},
		// Any phase can go to error
		{PhaseResearch, PhaseError, true},
		{PhasePlan, PhaseError, true},
		// Error can restart
		{PhaseError, PhasePending, true},
	}

	for _, tt := range tests {
		result := isValidTransition(tt.from, tt.to)
		if result != tt.valid {
			t.Errorf("isValidTransition(%q, %q) = %v, want %v", tt.from, tt.to, result, tt.valid)
		}
	}
}

func TestDeterminePhaseFromLabels(t *testing.T) {
	tests := []struct {
		labels []string
		want   string
	}{
		{[]string{"dayshift"}, ""},
		{[]string{"dayshift", "dayshift:researched"}, PhaseResearch},
		{[]string{"dayshift", "dayshift:planned", "dayshift:needs-input"}, PhaseClarify},
		{[]string{"dayshift", "dayshift:approved"}, PhaseImplement},
		{[]string{"dayshift", "dayshift:paused"}, PhasePaused},
		{[]string{"dayshift", "dayshift:complete"}, PhaseComplete},
	}

	for _, tt := range tests {
		got := determinePhaseFromLabels(tt.labels)
		if got != tt.want {
			t.Errorf("determinePhaseFromLabels(%v) = %q, want %q", tt.labels, got, tt.want)
		}
	}
}
