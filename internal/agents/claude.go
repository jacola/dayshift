package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// CommandRunner executes shell commands. Allows mocking in tests.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdin string) (stdout, stderr string, exitCode int, err error)
}

// ExecRunner is the default CommandRunner using os/exec.
type ExecRunner struct{}

func (r *ExecRunner) Run(ctx context.Context, name string, args []string, dir string, stdin string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	err := cmd.Run()

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}

// ClaudeAgent spawns Claude Code CLI for task execution.
type ClaudeAgent struct {
	binaryPath string
	timeout    time.Duration
	runner     CommandRunner
	skipPerms  bool
}

type ClaudeOption func(*ClaudeAgent)

func WithBinaryPath(path string) ClaudeOption {
	return func(a *ClaudeAgent) { a.binaryPath = path }
}

func WithDefaultTimeout(d time.Duration) ClaudeOption {
	return func(a *ClaudeAgent) { a.timeout = d }
}

func WithDangerouslySkipPermissions(enabled bool) ClaudeOption {
	return func(a *ClaudeAgent) { a.skipPerms = enabled }
}

func WithRunner(r CommandRunner) ClaudeOption {
	return func(a *ClaudeAgent) { a.runner = r }
}

func NewClaudeAgent(opts ...ClaudeOption) *ClaudeAgent {
	a := &ClaudeAgent{
		binaryPath: "claude",
		timeout:    DefaultTimeout,
		runner:     &ExecRunner{},
		skipPerms:  true,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *ClaudeAgent) Name() string { return "claude" }

func (a *ClaudeAgent) Execute(ctx context.Context, opts ExecuteOptions) (*ExecuteResult, error) {
	start := time.Now()

	timeout := a.timeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"--print"}
	if a.skipPerms {
		args = append(args, "--dangerously-skip-permissions")
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

func (a *ClaudeAgent) Available() bool {
	_, err := exec.LookPath(a.binaryPath)
	return err == nil
}

func (a *ClaudeAgent) Version() (string, error) {
	cmd := exec.Command(a.binaryPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// buildFileContext reads files and formats them as context.
func buildFileContext(files []string) (string, error) {
	var sb strings.Builder
	sb.WriteString("# Context Files\n\n")
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", path, err)
		}
		displayPath := path
		if abs, err := filepath.Abs(path); err == nil {
			displayPath = abs
		}
		fmt.Fprintf(&sb, "## File: %s\n\n```\n%s\n```\n\n", displayPath, string(content))
	}
	return sb.String(), nil
}

// extractJSON attempts to find and parse JSON from agent output.
func extractJSON(output []byte) []byte {
	if json.Valid(output) {
		return output
	}

	start := -1
	var opener, closer byte
	for i, b := range output {
		if b == '{' || b == '[' {
			start = i
			opener = b
			if b == '{' {
				closer = '}'
			} else {
				closer = ']'
			}
			break
		}
	}

	if start == -1 {
		return nil
	}

	depth := 0
	for i := start; i < len(output); i++ {
		if output[i] == opener {
			depth++
		} else if output[i] == closer {
			depth--
			if depth == 0 {
				candidate := output[start : i+1]
				if json.Valid(candidate) {
					return candidate
				}
				break
			}
		}
	}

	return nil
}
