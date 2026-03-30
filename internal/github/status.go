package github

import (
	"context"
	"fmt"
	"strings"
)

const (
	statusMarkerOpen  = "<!-- dayshift:status -->"
	statusMarkerClose = "<!-- /dayshift:status -->"
)

// StatusUpdate represents the current pipeline status for an issue.
type StatusUpdate struct {
	ResearchLink string
	PlanLink     string
	ImplementRef string
	ValidateLink string
}

// UpdateIssueStatus appends or updates the Dayshift Status section in the issue body.
func (c *Client) UpdateIssueStatus(ctx context.Context, repo string, number int, status StatusUpdate) error {
	issue, err := c.GetIssue(ctx, repo, number)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	statusSection := buildStatusSection(status)

	// Remove existing status section (between markers) if present
	body := issue.Body
	if startIdx := strings.Index(body, statusMarkerOpen); startIdx >= 0 {
		endIdx := strings.Index(body, statusMarkerClose)
		if endIdx >= 0 {
			body = body[:startIdx] + body[endIdx+len(statusMarkerClose):]
		} else {
			body = body[:startIdx]
		}
		body = strings.TrimRight(body, "\n")
	}

	newBody := body + "\n\n" + statusSection

	_, err = c.gh(ctx, repo,
		"issue", "edit", fmt.Sprintf("%d", number),
		"--body", newBody,
	)
	if err != nil {
		return fmt.Errorf("update issue body: %w", err)
	}
	return nil
}

// RemoveIssueStatus removes the Dayshift Status section from an issue body.
func (c *Client) RemoveIssueStatus(ctx context.Context, repo string, number int) error {
	issue, err := c.GetIssue(ctx, repo, number)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	body := issue.Body
	startIdx := strings.Index(body, statusMarkerOpen)
	if startIdx < 0 {
		return nil // no status to remove
	}

	endIdx := strings.Index(body, statusMarkerClose)
	if endIdx >= 0 {
		body = body[:startIdx] + body[endIdx+len(statusMarkerClose):]
	} else {
		body = body[:startIdx]
	}
	body = strings.TrimRight(body, "\n")

	_, err = c.gh(ctx, repo,
		"issue", "edit", fmt.Sprintf("%d", number),
		"--body", body,
	)
	if err != nil {
		return fmt.Errorf("update issue body: %w", err)
	}
	return nil
}

func buildStatusSection(s StatusUpdate) string {
	var lines []string

	if s.ResearchLink != "" {
		lines = append(lines, fmt.Sprintf("- [x] [Research Document](%s)", s.ResearchLink))
	} else {
		lines = append(lines, "- [ ] Research")
	}

	if s.PlanLink != "" {
		lines = append(lines, fmt.Sprintf("- [x] [Implementation Plan](%s)", s.PlanLink))
	} else {
		lines = append(lines, "- [ ] Implementation Plan")
	}

	if s.ImplementRef != "" {
		lines = append(lines, fmt.Sprintf("- [x] Implementation: %s", s.ImplementRef))
	} else {
		lines = append(lines, "- [ ] Implementation")
	}

	if s.ValidateLink != "" {
		lines = append(lines, fmt.Sprintf("- [x] [Validation Report](%s)", s.ValidateLink))
	} else {
		lines = append(lines, "- [ ] Validation Report")
	}

	return statusMarkerOpen + "\n\n---\n**Dayshift Status:**\n\n" + strings.Join(lines, "\n") + "\n" + statusMarkerClose
}
