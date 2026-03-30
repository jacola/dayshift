package github

import (
	"context"
	"fmt"
	"strings"
)

const statusSeparator = "\n\n---\n**Dayshift Status:**\n\n"

// StatusUpdate represents the current pipeline status for an issue.
type StatusUpdate struct {
	ResearchLink string // URL to research comment (empty = not done)
	PlanLink     string // URL to plan comment (empty = not done)
	ImplementRef string // PR reference like "#774" (empty = not done)
	ValidateLink string // URL to validation comment (empty = not done)
}

// UpdateIssueStatus appends or updates the Dayshift Status section in the issue body.
func (c *Client) UpdateIssueStatus(ctx context.Context, repo string, number int, status StatusUpdate) error {
	issue, err := c.GetIssue(ctx, repo, number)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	// Build status section
	statusSection := buildStatusSection(status)

	// Remove existing status section if present
	body := issue.Body
	if idx := strings.Index(body, "\n\n---\n**Dayshift Status:**"); idx >= 0 {
		body = body[:idx]
	}

	// Append new status
	newBody := body + statusSection

	// Update the issue body
	_, err = c.gh(ctx, repo,
		"issue", "edit", fmt.Sprintf("%d", number),
		"--body", newBody,
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

	return statusSeparator + strings.Join(lines, "\n") + "\n"
}
