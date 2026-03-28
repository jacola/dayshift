package agents

import (
	"context"
	"os"
	"testing"
	"time"
)

// MockRunner is a test double for CommandRunner.
type MockRunner struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
	LastArgs []string
	LastDir  string
}

func (m *MockRunner) Run(_ context.Context, name string, args []string, dir string, _ string) (string, string, int, error) {
	m.LastArgs = append([]string{name}, args...)
	m.LastDir = dir
	return m.Stdout, m.Stderr, m.ExitCode, m.Err
}

func TestClaudeAgentDefaults(t *testing.T) {
	agent := NewClaudeAgent()
	if agent.Name() != "claude" {
		t.Errorf("expected name=claude, got %s", agent.Name())
	}
	if agent.timeout != DefaultTimeout {
		t.Errorf("expected timeout=%v, got %v", DefaultTimeout, agent.timeout)
	}
	if !agent.skipPerms {
		t.Error("expected skipPerms=true by default")
	}
}

func TestClaudeAgentExecute(t *testing.T) {
	mock := &MockRunner{
		Stdout:   `{"result": "ok"}`,
		ExitCode: 0,
	}

	agent := NewClaudeAgent(WithRunner(mock))
	result, err := agent.Execute(context.Background(), ExecuteOptions{
		Prompt:  "test prompt",
		WorkDir: "/tmp",
	})

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsSuccess() {
		t.Errorf("expected success, got exitCode=%d error=%s", result.ExitCode, result.Error)
	}
	if result.JSON == nil {
		t.Error("expected JSON output to be parsed")
	}
}

func TestCodexAgentDefaults(t *testing.T) {
	agent := NewCodexAgent()
	if agent.Name() != "codex" {
		t.Errorf("expected name=codex, got %s", agent.Name())
	}
	if !agent.bypassPerm {
		t.Error("expected bypassPerm=true by default")
	}
}

func TestCopilotAgentDefaults(t *testing.T) {
	agent := NewCopilotAgent()
	if agent.Name() != "copilot" {
		t.Errorf("expected name=copilot, got %s", agent.Name())
	}
	if agent.binaryPath != "gh" {
		t.Errorf("expected binaryPath=gh, got %s", agent.binaryPath)
	}
	if agent.dangerouslySkipPerms {
		t.Error("expected dangerouslySkipPerms=false by default")
	}
}

func TestCopilotGhArgs(t *testing.T) {
	mock := &MockRunner{Stdout: "ok", ExitCode: 0}
	agent := NewCopilotAgent(WithCopilotRunner(mock))

	_, err := agent.Execute(context.Background(), ExecuteOptions{Prompt: "test"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Check that args include "copilot", "--", "-p"
	if len(mock.LastArgs) < 4 {
		t.Fatalf("expected at least 4 args, got %v", mock.LastArgs)
	}
	if mock.LastArgs[0] != "gh" || mock.LastArgs[1] != "copilot" || mock.LastArgs[2] != "--" {
		t.Errorf("unexpected args prefix: %v", mock.LastArgs[:3])
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"pure json", `{"key": "value"}`, true},
		{"json in text", `Some text {"key": "value"} more text`, true},
		{"json array", `[1, 2, 3]`, true},
		{"no json", "just plain text", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON([]byte(tt.input))
			if (result != nil) != tt.want {
				t.Errorf("extractJSON(%q) returned nil=%v, want nil=%v", tt.input, result == nil, !tt.want)
			}
		})
	}
}

func TestBuildFileContext(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.go"
	if err := os.WriteFile(tmpFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := buildFileContext([]string{tmpFile})
	if err != nil {
		t.Fatalf("buildFileContext: %v", err)
	}

	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestExecuteResultIsSuccess(t *testing.T) {
	r := &ExecuteResult{ExitCode: 0}
	if !r.IsSuccess() {
		t.Error("expected success for exit code 0")
	}

	r.Error = "something failed"
	if r.IsSuccess() {
		t.Error("expected failure when error is set")
	}

	r.Error = ""
	r.ExitCode = 1
	if r.IsSuccess() {
		t.Error("expected failure for non-zero exit code")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Error("should not truncate short string")
	}
	if truncate("hello world", 5) != "hello..." {
		t.Errorf("expected 'hello...', got %q", truncate("hello world", 5))
	}
}

func TestAgentTimeout(t *testing.T) {
	mock := &MockRunner{Stdout: "ok", ExitCode: 0}
	agent := NewClaudeAgent(
		WithRunner(mock),
		WithDefaultTimeout(1*time.Hour),
	)

	if agent.timeout != 1*time.Hour {
		t.Errorf("expected 1h timeout, got %v", agent.timeout)
	}
}
