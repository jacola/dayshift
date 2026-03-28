// Package github provides GitHub operations via the gh CLI.
package github

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/marcus/dayshift/internal/logging"
)

// CommandRunner executes shell commands. Allows mocking in tests.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string) (stdout, stderr string, exitCode int, err error)
}

// ExecRunner is the default CommandRunner using os/exec.
type ExecRunner struct{}

func (r *ExecRunner) Run(ctx context.Context, name string, args []string, dir string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}

// Client provides GitHub operations via the gh CLI.
type Client struct {
	runner   CommandRunner
	repoDirs map[string]string // repo -> local directory mapping
	logger   *logging.Logger
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithRunner sets a custom command runner (for testing).
func WithRunner(r CommandRunner) ClientOption {
	return func(c *Client) { c.runner = r }
}

// WithRepoDirs sets the repo-to-directory mapping.
func WithRepoDirs(dirs map[string]string) ClientOption {
	return func(c *Client) { c.repoDirs = dirs }
}

// WithLogger sets the logger.
func WithLogger(l *logging.Logger) ClientOption {
	return func(c *Client) { c.logger = l }
}

// NewClient creates a new GitHub client.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		runner:   &ExecRunner{},
		repoDirs: make(map[string]string),
		logger:   logging.Component("github"),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// repoDir returns the local directory for a repo, or empty string if not mapped.
func (c *Client) repoDir(repo string) string {
	return c.repoDirs[repo]
}

// gh runs a gh CLI command and returns stdout.
func (c *Client) gh(ctx context.Context, repo string, args ...string) (string, error) {
	dir := c.repoDir(repo)
	// If no local dir, add -R flag for repo targeting
	if dir == "" {
		args = append([]string{"-R", repo}, args...)
	}
	stdout, stderr, exitCode, err := c.runner.Run(ctx, "gh", args, dir)
	if err != nil {
		if exitCode != 0 {
			return "", fmt.Errorf("gh %s: exit %d: %s", strings.Join(args, " "), exitCode, strings.TrimSpace(stderr))
		}
		return "", fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return stdout, nil
}

// Available checks if the gh CLI is available in PATH.
func (c *Client) Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}
