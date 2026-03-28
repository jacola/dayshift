package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
	ID         string    `json:"id"`
	DatabaseID int       `json:"databaseId"`
	Body       string    `json:"body"`
	Author     GHAuthor  `json:"author"`
	CreatedAt  time.Time `json:"createdAt"`
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

	// gh issue view doesn't return databaseId, so fetch via API if needed
	if len(comments) > 0 && comments[0].DatabaseID == 0 {
		apiComments, err := c.getCommentsViaAPI(ctx, repo, number)
		if err == nil {
			// Match by position (both return in chronological order)
			for i := range comments {
				if i < len(apiComments) {
					comments[i].DatabaseID = apiComments[i].DatabaseID
				}
			}
		}
	}

	return comments, nil
}

// getCommentsViaAPI fetches comments with numeric IDs via REST API.
func (c *Client) getCommentsViaAPI(ctx context.Context, repo string, number int) ([]Comment, error) {
	stdout, err := c.gh(ctx, repo,
		"api", fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number),
		"--jq", "[.[] | {databaseId: .id, body: .body, author: {login: .user.login}, createdAt: .created_at}]",
	)
	if err != nil {
		return nil, err
	}
	var comments []Comment
	if err := json.Unmarshal([]byte(stdout), &comments); err != nil {
		return nil, err
	}
	return comments, nil
}

// EditComment updates an existing comment by its numeric database ID.
func (c *Client) EditComment(ctx context.Context, repo string, commentDatabaseID int, body string) error {
	_, err := c.gh(ctx, repo,
		"api", "--method", "PATCH",
		fmt.Sprintf("/repos/%s/issues/comments/%d", repo, commentDatabaseID),
		"-f", "body="+body,
	)
	if err != nil {
		return fmt.Errorf("edit comment %d: %w", commentDatabaseID, err)
	}
	return nil
}

// FindCommentByMarker finds a comment containing a specific marker and returns it.
func (c *Client) FindCommentByMarker(ctx context.Context, repo string, number int, marker string) (*Comment, error) {
	ghComments, err := c.GetComments(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	// Return the last matching comment
	for i := len(ghComments) - 1; i >= 0; i-- {
		if strings.Contains(ghComments[i].Body, marker) {
			return &ghComments[i], nil
		}
	}
	return nil, nil
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
