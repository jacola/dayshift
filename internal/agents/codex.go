package agents

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CodexAgent spawns Codex CLI for task execution.
type CodexAgent struct {
	binaryPath string
	timeout    time.Duration
	runner     CommandRunner
	bypassPerm bool
}

type CodexOption func(*CodexAgent)

func WithCodexBinaryPath(path string) CodexOption {
	return func(a *CodexAgent) { a.binaryPath = path }
}

func WithCodexDefaultTimeout(d time.Duration) CodexOption {
	return func(a *CodexAgent) { a.timeout = d }
}

func WithDangerouslyBypassApprovalsAndSandbox(enabled bool) CodexOption {
	return func(a *CodexAgent) { a.bypassPerm = enabled }
}

func WithCodexRunner(r CommandRunner) CodexOption {
	return func(a *CodexAgent) { a.runner = r }
}

func NewCodexAgent(opts ...CodexOption) *CodexAgent {
	a := &CodexAgent{
		binaryPath: "codex",
		timeout:    DefaultTimeout,
		runner:     &ExecRunner{},
		bypassPerm: true,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *CodexAgent) Name() string { return "codex" }

func (a *CodexAgent) Execute(ctx context.Context, opts ExecuteOptions) (*ExecuteResult, error) {
	start := time.Now()

	timeout := a.timeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"exec"}
	if a.bypassPerm {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}
	if opts.Prompt != "" {
		args = append(args, opts.Prompt)
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
		if stderr != "" {
			result.Error = fmt.Sprintf("timeout after %v; stderr: %s", timeout, truncate(stderr, 2000))
		}
		result.ExitCode = -1
		return result, ctx.Err()
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			result.Error = stderr
		} else {
			result.Error = err.Error()
			if stderr != "" {
				result.Error = fmt.Sprintf("%s; stderr: %s", err.Error(), truncate(stderr, 2000))
			}
		}
		return result, err
	}

	result.JSON = extractJSON([]byte(stdout))
	return result, nil
}

func (a *CodexAgent) Available() bool {
	_, err := exec.LookPath(a.binaryPath)
	return err == nil
}

func (a *CodexAgent) Version() (string, error) {
	cmd := exec.Command(a.binaryPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
