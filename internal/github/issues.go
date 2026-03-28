package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Issue represents a GitHub issue.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Labels    []GHLabel `json:"labels"`
	State     string    `json:"state"`
	Author    GHAuthor  `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GHLabel represents a GitHub label.
type GHLabel struct {
	Name string `json:"name"`
}

// GHAuthor represents a GitHub user.
type GHAuthor struct {
	Login string `json:"login"`
}

// LabelNames returns label names as a string slice.
func (i *Issue) LabelNames() []string {
	names := make([]string, len(i.Labels))
	for j, l := range i.Labels {
		names[j] = l.Name
	}
	return names
}

// HasLabel checks if the issue has a specific label.
func (i *Issue) HasLabel(name string) bool {
	for _, l := range i.Labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

// Comment represents a GitHub issue comment.
type Comment struct {
	ID        int       `json:"id"`
	Body      string    `json:"body"`
	Author    GHAuthor  `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
}

// ListIssues lists open issues with a specific label.
func (c *Client) ListIssues(ctx context.Context, repo string, label string) ([]Issue, error) {
	stdout, err := c.gh(ctx, repo,
		"issue", "list",
		"--label", label,
		"--state", "open",
		"--json", "number,title,body,labels,state,author,createdAt,updatedAt",
		"--limit", "100",
	)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	var issues []Issue
	if err := json.Unmarshal([]byte(stdout), &issues); err != nil {
		return nil, fmt.Errorf("parse issues: %w", err)
	}
	return issues, nil
}

// GetIssue fetches a single issue by number.
func (c *Client) GetIssue(ctx context.Context, repo string, number int) (*Issue, error) {
	stdout, err := c.gh(ctx, repo,
		"issue", "view", strconv.Itoa(number),
		"--json", "number,title,body,labels,state,author,createdAt,updatedAt",
	)
	if err != nil {
		return nil, fmt.Errorf("get issue %d: %w", number, err)
	}

	var issue Issue
	if err := json.Unmarshal([]byte(stdout), &issue); err != nil {
		return nil, fmt.Errorf("parse issue %d: %w", number, err)
	}
	return &issue, nil
}

// PostComment posts a comment on an issue.
func (c *Client) PostComment(ctx context.Context, repo string, number int, body string) error {
	_, err := c.gh(ctx, repo,
		"issue", "comment", strconv.Itoa(number),
		"--body", body,
	)
	if err != nil {
		return fmt.Errorf("post comment on #%d: %w", number, err)
	}
	return nil
}

// GetComments retrieves all comments on an issue.
func (c *Client) GetComments(ctx context.Context, repo string, number int) ([]Comment, error) {
	stdout, err := c.gh(ctx, repo,
		"issue", "view", strconv.Itoa(number),
		"--json", "comments",
		"--jq", ".comments",
	)
	if err != nil {
		return nil, fmt.Errorf("get comments for #%d: %w", number, err)
	}

	var comments []Comment
	if stdout == "" || stdout == "null" {
		return comments, nil
	}
	if err := json.Unmarshal([]byte(stdout), &comments); err != nil {
		return nil, fmt.Errorf("parse comments for #%d: %w", number, err)
	}
	return comments, nil
}

// GetCommentsSince retrieves comments posted after a given time.
func (c *Client) GetCommentsSince(ctx context.Context, repo string, number int, since time.Time) ([]Comment, error) {
	all, err := c.GetComments(ctx, repo, number)
	if err != nil {
		return nil, err
	}

	var filtered []Comment
	for _, comment := range all {
		if comment.CreatedAt.After(since) {
			filtered = append(filtered, comment)
		}
	}
	return filtered, nil
}
