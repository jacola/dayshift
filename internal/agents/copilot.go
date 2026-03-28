package agents

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CopilotAgent spawns GitHub Copilot CLI for task execution.
type CopilotAgent struct {
	binaryPath           string
	dangerouslySkipPerms bool
	timeout              time.Duration
	runner               CommandRunner
}

type CopilotOption func(*CopilotAgent)

func WithCopilotBinaryPath(path string) CopilotOption {
	return func(a *CopilotAgent) { a.binaryPath = path }
}

func WithCopilotDangerouslySkipPermissions(enabled bool) CopilotOption {
	return func(a *CopilotAgent) { a.dangerouslySkipPerms = enabled }
}

func WithCopilotDefaultTimeout(d time.Duration) CopilotOption {
	return func(a *CopilotAgent) { a.timeout = d }
}

func WithCopilotRunner(r CommandRunner) CopilotOption {
	return func(a *CopilotAgent) { a.runner = r }
}

func NewCopilotAgent(opts ...CopilotOption) *CopilotAgent {
	a := &CopilotAgent{
		binaryPath: "gh",
		timeout:    DefaultTimeout,
		runner:     &ExecRunner{},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *CopilotAgent) Name() string { return "copilot" }

func (a *CopilotAgent) Execute(ctx context.Context, opts ExecuteOptions) (*ExecuteResult, error) {
	start := time.Now()

	timeout := a.timeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var args []string
	if a.binaryPath == "gh" {
		args = []string{"copilot", "--", "-p", opts.Prompt, "--no-ask-user", "--silent"}
		if a.dangerouslySkipPerms {
			args = append(args, "--allow-all-tools", "--allow-all-urls")
		}
	} else {
		args = []string{"-p", opts.Prompt, "--no-ask-user", "--silent"}
		if a.dangerouslySkipPerms {
			args = append(args, "--allow-all-tools", "--allow-all-urls")
		}
	}

	var stdinContent string
	if len(opts.Files) > 0 {
		var err error
		stdinContent, err = buildFileContext(opts.Files)
		if err != nil {
			return &ExecuteResult{
				Error:    fmt.Sprintf("building file context: %v", err),
				Duration: time.Since(start),
			}, err
		}
	}

	stdout, stderr, exitCode, err := a.runner.Run(ctx, a.binaryPath, args, opts.WorkDir, stdinContent)

	result := &ExecuteResult{
		Output:   stdout,
		ExitCode: exitCode,
		Duration: time.Since(start),
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.Error = fmt.Sprintf("timeout after %v", timeout)
		result.ExitCode = -1
		return result, ctx.Err()
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			result.Error = stderr
		} else {
			result.Error = err.Error()
		}
		return result, err
	}

	result.JSON = extractJSON([]byte(stdout))
	return result, nil
}

func (a *CopilotAgent) Available() bool {
	if _, err := exec.LookPath(a.binaryPath); err != nil {
		return false
	}
	if a.binaryPath == "copilot" {
		return true
	}
	cmd := exec.Command(a.binaryPath, "extension", "list")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "github/gh-copilot") ||
		strings.Contains(string(output), "gh-copilot")
}

func (a *CopilotAgent) Version() (string, error) {
	var cmd *exec.Cmd
	if a.binaryPath == "gh" {
		cmd = exec.Command("gh", "copilot", "--", "--version")
	} else {
		cmd = exec.Command(a.binaryPath, "--version")
	}
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
