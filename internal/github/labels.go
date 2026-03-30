package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// LabelDef defines a GitHub label.
type LabelDef struct {
	Name        string
	Color       string
	Description string
}

// DayshiftLabels defines all labels used by the dayshift pipeline.
var DayshiftLabels = []LabelDef{
	{Name: "dayshift", Color: "0E8A16", Description: "Process with dayshift"},
	{Name: "dayshift:researched", Color: "1D76DB", Description: "Research phase complete"},
	{Name: "dayshift:planned", Color: "1D76DB", Description: "Plan phase complete"},
	{Name: "dayshift:needs-input", Color: "FBCA04", Description: "Agent needs human input"},
	{Name: "dayshift:awaiting-approval", Color: "FBCA04", Description: "Deprecated: was used for approval phase"},
	{Name: "dayshift:approved", Color: "0E8A16", Description: "Deprecated: was used for approval phase"},
	{Name: "dayshift:implementing", Color: "D93F0B", Description: "Implementation in progress"},
	{Name: "dayshift:implemented", Color: "1D76DB", Description: "Implementation complete"},
	{Name: "dayshift:validated", Color: "0E8A16", Description: "Validation passed"},
	{Name: "dayshift:needs-fixes", Color: "D93F0B", Description: "Validation found issues"},
	{Name: "dayshift:complete", Color: "0E8A16", Description: "All phases done"},
	{Name: "dayshift:error", Color: "B60205", Description: "Processing failed"},
	{Name: "dayshift:paused", Color: "C5DEF5", Description: "Manually paused"},
}

// EnsureLabels creates all dayshift labels on a repository, skipping any that already exist.
func (c *Client) EnsureLabels(ctx context.Context, repo string) error {
	// Get existing labels
	existing, err := c.listRepoLabels(ctx, repo)
	if err != nil {
		return fmt.Errorf("list existing labels: %w", err)
	}

	existingSet := make(map[string]bool)
	for _, name := range existing {
		existingSet[name] = true
	}

	for _, label := range DayshiftLabels {
		if existingSet[label.Name] {
			c.logger.Debugf("label %q already exists on %s", label.Name, repo)
			continue
		}

		_, err := c.gh(ctx, repo,
			"label", "create", label.Name,
			"--color", label.Color,
			"--description", label.Description,
			"--force",
		)
		if err != nil {
			return fmt.Errorf("create label %q: %w", label.Name, err)
		}
		c.logger.Infof("created label %q on %s", label.Name, repo)
	}
	return nil
}

// listRepoLabels returns all label names on a repository.
func (c *Client) listRepoLabels(ctx context.Context, repo string) ([]string, error) {
	stdout, err := c.gh(ctx, repo,
		"label", "list",
		"--json", "name",
		"--limit", "200",
	)
	if err != nil {
		return nil, err
	}

	var labels []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(stdout), &labels); err != nil {
		return nil, fmt.Errorf("parse labels: %w", err)
	}

	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return names, nil
}

// AddLabel adds a label to an issue.
func (c *Client) AddLabel(ctx context.Context, repo string, number int, label string) error {
	_, err := c.gh(ctx, repo,
		"issue", "edit", strconv.Itoa(number),
		"--add-label", label,
	)
	if err != nil {
		return fmt.Errorf("add label %q to #%d: %w", label, number, err)
	}
	return nil
}

// RemoveLabel removes a label from an issue.
func (c *Client) RemoveLabel(ctx context.Context, repo string, number int, label string) error {
	_, err := c.gh(ctx, repo,
		"issue", "edit", strconv.Itoa(number),
		"--remove-label", label,
	)
	if err != nil {
		// Ignore errors from removing labels that don't exist
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return fmt.Errorf("remove label %q from #%d: %w", label, number, err)
	}
	return nil
}

// GetLabels returns the labels on a specific issue.
func (c *Client) GetLabels(ctx context.Context, repo string, number int) ([]string, error) {
	issue, err := c.GetIssue(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	return issue.LabelNames(), nil
}
