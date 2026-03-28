package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAndMigrate(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	if database.SQL() == nil {
		t.Fatal("expected non-nil sql.DB")
	}

	if database.Path() != dbPath {
		t.Errorf("expected path %s, got %s", dbPath, database.Path())
	}

	// Verify schema was created
	version, err := CurrentVersion(database.SQL())
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if version != 1 {
		t.Errorf("expected version 1, got %d", version)
	}

	// Verify tables exist
	tables := []string{"issues", "issue_comments", "phase_history", "run_history", "schema_version"}
	for _, table := range tables {
		var name string
		err := database.SQL().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}
}

func TestInsertAndQueryIssue(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	// Insert an issue
	_, err = database.SQL().Exec(
		`INSERT INTO issues (repo, issue_number, title, phase) VALUES (?, ?, ?, ?)`,
		"owner/repo", 42, "Test issue", "research",
	)
	if err != nil {
		t.Fatalf("insert issue: %v", err)
	}

	// Query it back
	var repo string
	var number int
	var title string
	var phase string
	err = database.SQL().QueryRow(
		`SELECT repo, issue_number, title, phase FROM issues WHERE issue_number = ?`, 42,
	).Scan(&repo, &number, &title, &phase)
	if err != nil {
		t.Fatalf("query issue: %v", err)
	}

	if repo != "owner/repo" || number != 42 || title != "Test issue" || phase != "research" {
		t.Errorf("unexpected values: repo=%s number=%d title=%s phase=%s", repo, number, title, phase)
	}
}

func TestUniqueConstraint(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	_, err = database.SQL().Exec(
		`INSERT INTO issues (repo, issue_number, title) VALUES (?, ?, ?)`,
		"owner/repo", 1, "First",
	)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}

	_, err = database.SQL().Exec(
		`INSERT INTO issues (repo, issue_number, title) VALUES (?, ?, ?)`,
		"owner/repo", 1, "Duplicate",
	)
	if err == nil {
		t.Fatal("expected unique constraint violation")
	}
}

func TestPhaseHistory(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	// Insert issue
	res, err := database.SQL().Exec(
		`INSERT INTO issues (repo, issue_number, title) VALUES (?, ?, ?)`,
		"owner/repo", 1, "Test",
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	issueID, _ := res.LastInsertId()

	// Record phase transition
	_, err = database.SQL().Exec(
		`INSERT INTO phase_history (issue_id, from_phase, to_phase, reason) VALUES (?, ?, ?, ?)`,
		issueID, "pending", "research", "new issue",
	)
	if err != nil {
		t.Fatalf("insert phase_history: %v", err)
	}

	var fromPhase, toPhase, reason string
	err = database.SQL().QueryRow(
		`SELECT from_phase, to_phase, reason FROM phase_history WHERE issue_id = ?`, issueID,
	).Scan(&fromPhase, &toPhase, &reason)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if fromPhase != "pending" || toPhase != "research" || reason != "new issue" {
		t.Errorf("unexpected: from=%s to=%s reason=%s", fromPhase, toPhase, reason)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sqlDB.Close()

	// Run migrations twice
	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	version, err := CurrentVersion(sqlDB)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if version != 1 {
		t.Errorf("expected version 1, got %d", version)
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~/test", filepath.Join(home, "test")},
		{"~", home},
	}

	for _, tt := range tests {
		result := expandPath(tt.input)
		if result != tt.expected {
			t.Errorf("expandPath(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
