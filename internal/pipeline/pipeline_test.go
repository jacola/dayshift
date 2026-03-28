package pipeline

import (
	"strings"
	"testing"

	"github.com/marcus/dayshift/internal/config"
	gh "github.com/marcus/dayshift/internal/github"
	"github.com/marcus/dayshift/internal/scanner"
)

func TestExtractPRURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"simple",
			"Created PR: https://github.com/owner/repo/pull/42",
			"https://github.com/owner/repo/pull/42",
		},
		{
			"multiple — returns last",
			"See https://github.com/owner/repo/pull/1 and https://github.com/owner/repo/pull/42",
			"https://github.com/owner/repo/pull/42",
		},
		{
			"no PR",
			"Just some text with no PR URL",
			"",
		},
		{
			"in markdown",
			"[PR #42](https://github.com/owner/repo/pull/42) is ready",
			"https://github.com/owner/repo/pull/42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPRURL(tt.input)
			if got != tt.want {
				t.Errorf("extractPRURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferValidationPassed(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		passed bool
	}{
		{"clear pass", "PASSED: All checks verified", true},
		{"clear fail", "FAILED: Missing test coverage", false},
		{"mixed — more pass", "The implementation looks good and is correct. One minor issue found.", true},
		{"mixed — more fail", "FAILED. Bug found, missing tests, incomplete implementation.", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferValidationPassed(tt.input)
			if got != tt.passed {
				t.Errorf("inferValidationPassed() = %v, want %v", got, tt.passed)
			}
		})
	}
}

func TestBuildResearchPrompt(t *testing.T) {
	work := createTestWork()
	prompt := buildResearchPrompt(work)

	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !containsAll(prompt, "Test issue", "#42", "owner/repo", "Fix the thing") {
		t.Error("prompt missing expected content")
	}
}

func TestBuildPlanPrompt(t *testing.T) {
	work := createTestWork()
	prompt := buildPlanPrompt(work, "Research findings here", "", "")

	if !containsAll(prompt, "Test issue", "Research findings here", "dayshift:questions") {
		t.Error("prompt missing expected content")
	}

	// With existing plan and human answers
	prompt = buildPlanPrompt(work, "Research", "Existing plan here", "1: Use PostgreSQL")
	if !containsAll(prompt, "Existing Plan", "Existing plan here", "Human Answers", "1: Use PostgreSQL") {
		t.Error("prompt missing existing plan or human answers")
	}
}

func TestBuildImplementPrompt(t *testing.T) {
	work := createTestWork()
	prompt := buildImplementPrompt(work, "Research", "Plan", "main")

	if !containsAll(prompt, "Test issue", "Research", "Plan", "main", "Fixes #42", "Dayshift-Issue") {
		t.Error("prompt missing expected content")
	}
}

func TestBuildValidatePrompt(t *testing.T) {
	work := createTestWork()
	prompt := buildValidatePrompt(work, "Plan", "https://github.com/owner/repo/pull/1")

	if !containsAll(prompt, "Test issue", "Plan", "pull/1", "PASSED or FAILED") {
		t.Error("prompt missing expected content")
	}
}

// helpers

func createTestWork() scanner.PendingWork {
	return scanner.PendingWork{
		Issue: createTestIssue(),
		Project: config.ProjectConfig{
			Repo: "owner/repo",
			Path: "/test/repo",
		},
	}
}

func createTestIssue() gh.Issue {
	return gh.Issue{
		Number: 42,
		Title:  "Test issue",
		Body:   "Fix the thing",
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
