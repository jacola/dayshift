package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// MockRunner records calls and returns canned responses.
type MockRunner struct {
	Calls    []MockCall
	Response map[string]MockResponse
}

type MockCall struct {
	Name string
	Args []string
	Dir  string
}

type MockResponse struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

func (m *MockRunner) Run(ctx context.Context, name string, args []string, dir string) (string, string, int, error) {
	call := MockCall{Name: name, Args: args, Dir: dir}
	m.Calls = append(m.Calls, call)

	key := strings.Join(append([]string{name}, args...), " ")
	if resp, ok := m.Response[key]; ok {
		return resp.Stdout, resp.Stderr, resp.ExitCode, resp.Err
	}

	// Try matching by first few args
	for k, resp := range m.Response {
		if strings.HasPrefix(key, k) {
			return resp.Stdout, resp.Stderr, resp.ExitCode, resp.Err
		}
	}

	return "", "", 0, nil
}

func NewMockRunner() *MockRunner {
	return &MockRunner{
		Response: make(map[string]MockResponse),
	}
}

func TestListIssues(t *testing.T) {
	mock := NewMockRunner()
	issues := []Issue{
		{Number: 1, Title: "Test issue", State: "open", Labels: []GHLabel{{Name: "dayshift"}}},
		{Number: 2, Title: "Another issue", State: "open", Labels: []GHLabel{{Name: "dayshift"}, {Name: "bug"}}},
	}
	data, _ := json.Marshal(issues)

	mock.Response["gh -R owner/repo issue list"] = MockResponse{Stdout: string(data)}

	client := NewClient(WithRunner(mock))
	result, err := client.ListIssues(context.Background(), "owner/repo", "dayshift", "")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(result))
	}
	if result[0].Title != "Test issue" {
		t.Errorf("expected title 'Test issue', got %q", result[0].Title)
	}
}

func TestGetIssue(t *testing.T) {
	mock := NewMockRunner()
	issue := Issue{Number: 42, Title: "Feature request", State: "open", Body: "Please add X"}
	data, _ := json.Marshal(issue)

	mock.Response["gh -R owner/repo issue view 42"] = MockResponse{Stdout: string(data)}

	client := NewClient(WithRunner(mock))
	result, err := client.GetIssue(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if result.Title != "Feature request" {
		t.Errorf("expected title 'Feature request', got %q", result.Title)
	}
}

func TestPostComment(t *testing.T) {
	mock := NewMockRunner()
	mock.Response["gh -R owner/repo issue comment 1"] = MockResponse{Stdout: "https://github.com/owner/repo/issues/1#issuecomment-123"}

	client := NewClient(WithRunner(mock))
	url, err := client.PostComment(context.Background(), "owner/repo", 1, "Test comment")
	if err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	if url != "https://github.com/owner/repo/issues/1#issuecomment-123" {
		t.Errorf("expected comment URL, got %q", url)
	}

	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}
	args := mock.Calls[0].Args
	found := false
	for _, a := range args {
		if a == "Test comment" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected comment body in args")
	}
}

func TestIssueHasLabel(t *testing.T) {
	issue := &Issue{Labels: []GHLabel{{Name: "dayshift"}, {Name: "bug"}}}
	if !issue.HasLabel("dayshift") {
		t.Error("expected HasLabel('dayshift') to be true")
	}
	if issue.HasLabel("feature") {
		t.Error("expected HasLabel('feature') to be false")
	}
}

func TestIssueLabelNames(t *testing.T) {
	issue := &Issue{Labels: []GHLabel{{Name: "dayshift"}, {Name: "bug"}}}
	names := issue.LabelNames()
	if len(names) != 2 || names[0] != "dayshift" || names[1] != "bug" {
		t.Errorf("expected [dayshift bug], got %v", names)
	}
}

func TestDayshiftLabelsCount(t *testing.T) {
	if len(DayshiftLabels) != 13 {
		t.Errorf("expected 13 labels, got %d", len(DayshiftLabels))
	}
}

func TestAddLabel(t *testing.T) {
	mock := NewMockRunner()
	mock.Response["gh -R owner/repo issue edit 1"] = MockResponse{Stdout: ""}

	client := NewClient(WithRunner(mock))
	err := client.AddLabel(context.Background(), "owner/repo", 1, "dayshift:researched")
	if err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
}

func TestRemoveLabel(t *testing.T) {
	mock := NewMockRunner()
	mock.Response["gh -R owner/repo issue edit 1"] = MockResponse{Stdout: ""}

	client := NewClient(WithRunner(mock))
	err := client.RemoveLabel(context.Background(), "owner/repo", 1, "dayshift:researched")
	if err != nil {
		t.Fatalf("RemoveLabel: %v", err)
	}
}

func TestEnsureLabels(t *testing.T) {
	mock := NewMockRunner()
	// Return empty list of existing labels
	mock.Response["gh -R owner/repo label list"] = MockResponse{Stdout: "[]"}
	// All label create calls succeed
	for _, label := range DayshiftLabels {
		key := fmt.Sprintf("gh -R owner/repo label create %s", label.Name)
		mock.Response[key] = MockResponse{Stdout: ""}
	}

	client := NewClient(WithRunner(mock))
	err := client.EnsureLabels(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}

	// Should have 1 list call + 13 create calls = 14 calls
	if len(mock.Calls) != 14 {
		t.Errorf("expected 14 gh calls, got %d", len(mock.Calls))
	}
}

func TestEnsureLabelsSkipsExisting(t *testing.T) {
	mock := NewMockRunner()
	// Return list with "dayshift" already existing
	existing := []struct{ Name string }{{Name: "dayshift"}}
	data, _ := json.Marshal(existing)
	mock.Response["gh -R owner/repo label list"] = MockResponse{Stdout: string(data)}
	// Other label create calls succeed
	for _, label := range DayshiftLabels {
		if label.Name == "dayshift" {
			continue
		}
		key := fmt.Sprintf("gh -R owner/repo label create %s", label.Name)
		mock.Response[key] = MockResponse{Stdout: ""}
	}

	client := NewClient(WithRunner(mock))
	err := client.EnsureLabels(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}

	// Should have 1 list call + 12 create calls (skipping "dayshift") = 13 calls
	if len(mock.Calls) != 13 {
		t.Errorf("expected 13 gh calls, got %d", len(mock.Calls))
	}
}

func TestClientWithRepoDir(t *testing.T) {
	mock := NewMockRunner()
	issue := Issue{Number: 1, Title: "Test", State: "open"}
	data, _ := json.Marshal(issue)
	// When using repo dir, no -R flag is added
	mock.Response["gh issue view 1"] = MockResponse{Stdout: string(data)}

	client := NewClient(
		WithRunner(mock),
		WithRepoDirs(map[string]string{"owner/repo": "/path/to/repo"}),
	)
	_, err := client.GetIssue(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatalf("GetIssue with repoDir: %v", err)
	}

	if mock.Calls[0].Dir != "/path/to/repo" {
		t.Errorf("expected dir=/path/to/repo, got %q", mock.Calls[0].Dir)
	}
}
