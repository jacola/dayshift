// Package agents provides interfaces and implementations for spawning AI agents.
package agents

import (
	"context"
	"time"
)

// DefaultTimeout is the default agent execution timeout (30 minutes).
const DefaultTimeout = 30 * time.Minute

// Agent is the interface for AI agent execution.
type Agent interface {
	Name() string
	Execute(ctx context.Context, opts ExecuteOptions) (*ExecuteResult, error)
}

// ExecuteOptions configures an agent execution.
type ExecuteOptions struct {
	Prompt  string
	WorkDir string
	Files   []string
	Timeout time.Duration
}

// ExecuteResult holds the outcome of an agent execution.
type ExecuteResult struct {
	Output   string
	JSON     []byte
	ExitCode int
	Duration time.Duration
	Error    string
}

// IsSuccess returns true if the execution succeeded.
func (r *ExecuteResult) IsSuccess() bool {
	return r.ExitCode == 0 && r.Error == ""
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
