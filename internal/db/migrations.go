package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
)

// Migration represents a single schema change.
type Migration struct {
	Version     int
	Description string
	SQL         string
}

var migrations = []Migration{
	{
		Version:     1,
		Description: "initial schema: issues, issue_comments, phase_history, run_history",
		SQL:         migration001SQL,
	},
}

const migration001SQL = `
CREATE TABLE issues (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    repo          TEXT NOT NULL,
    issue_number  INTEGER NOT NULL,
    title         TEXT,
    phase         TEXT NOT NULL DEFAULT 'pending',
    phase_started DATETIME,
    phase_data    TEXT,
    pr_url        TEXT,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(repo, issue_number)
);

CREATE TABLE issue_comments (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id      INTEGER REFERENCES issues(id),
    phase         TEXT NOT NULL,
    comment_id    INTEGER,
    content       TEXT,
    author        TEXT,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE phase_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id      INTEGER REFERENCES issues(id),
    from_phase    TEXT,
    to_phase      TEXT,
    reason        TEXT,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE run_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    start_time    DATETIME,
    end_time      DATETIME,
    provider      TEXT,
    repo          TEXT,
    issues_processed INTEGER DEFAULT 0,
    status        TEXT,
    error         TEXT
);

CREATE INDEX idx_issues_repo_phase ON issues(repo, phase);
CREATE INDEX idx_issues_phase ON issues(phase);
CREATE INDEX idx_issue_comments_issue ON issue_comments(issue_id);
CREATE INDEX idx_phase_history_issue ON phase_history(issue_id);
CREATE INDEX idx_run_history_time ON run_history(start_time DESC);
`

// Migrate runs all pending migrations inside transactions.
func Migrate(db *sql.DB) error {
	if db == nil {
		return errors.New("db is nil")
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY, applied_at DATETIME)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	currentVersion, err := CurrentVersion(db)
	if err != nil {
		return err
	}

	for _, migration := range migrations {
		if migration.Version <= currentVersion {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.Version, err)
		}

		if _, err := tx.Exec(migration.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", migration.Version, err)
		}

		if _, err := tx.Exec(`INSERT INTO schema_version (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, migration.Version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.Version, err)
		}

		log.Printf("db: applied migration %d: %s", migration.Version, migration.Description)
	}

	return nil
}

// CurrentVersion returns the current schema version (0 if no migrations applied).
func CurrentVersion(db *sql.DB) (int, error) {
	if db == nil {
		return 0, errors.New("db is nil")
	}
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	var version int
	if err := row.Scan(&version); err != nil {
		return 0, fmt.Errorf("query schema_version: %w", err)
	}
	return version, nil
}
