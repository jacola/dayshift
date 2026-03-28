package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/marcus/dayshift/internal/agents"
	"github.com/marcus/dayshift/internal/config"
	"github.com/marcus/dayshift/internal/db"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check configuration and environment health",
	Long:  `Verify that dayshift is properly configured and all dependencies are available.`,
	RunE:  runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Print("\n🩺 Dayshift Doctor\n\n")

	passed := 0
	warned := 0
	failed := 0

	check := func(name string, fn func() (string, error)) {
		result, err := fn()
		if err != nil {
			fmt.Printf("  ✗ %s: %v\n", name, err)
			failed++
		} else if strings.HasPrefix(result, "WARN:") {
			fmt.Printf("  ⚠ %s: %s\n", name, strings.TrimPrefix(result, "WARN:"))
			warned++
		} else {
			fmt.Printf("  ✓ %s: %s\n", name, result)
			passed++
		}
	}

	// Config file
	check("Config file", func() (string, error) {
		path := config.GlobalConfigPath()
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return "", fmt.Errorf("not found at %s (run 'dayshift init')", path)
		}
		cfg, err := config.Load()
		if err != nil {
			return "", fmt.Errorf("invalid: %v", err)
		}
		return fmt.Sprintf("valid (%d projects)", len(cfg.Projects)), nil
	})

	// gh CLI
	check("gh CLI", func() (string, error) {
		path, err := exec.LookPath("gh")
		if err != nil {
			return "", fmt.Errorf("not found in PATH")
		}
		out, err := exec.Command("gh", "auth", "status").CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("not authenticated: %s", strings.TrimSpace(string(out)))
		}
		return fmt.Sprintf("available at %s", path), nil
	})

	// AI providers
	check("Claude CLI", func() (string, error) {
		a := agents.NewClaudeAgent()
		if a.Available() {
			v, _ := a.Version()
			if v != "" {
				return v, nil
			}
			return "available", nil
		}
		return "WARN: not found in PATH", nil
	})

	check("Codex CLI", func() (string, error) {
		a := agents.NewCodexAgent()
		if a.Available() {
			v, _ := a.Version()
			if v != "" {
				return v, nil
			}
			return "available", nil
		}
		return "WARN: not found in PATH", nil
	})

	check("Copilot CLI", func() (string, error) {
		a := agents.NewCopilotAgent()
		if a.Available() {
			v, _ := a.Version()
			if v != "" {
				return v, nil
			}
			return "available", nil
		}
		return "WARN: not found in PATH", nil
	})

	// Database
	check("SQLite database", func() (string, error) {
		database, err := db.Open("")
		if err != nil {
			return "", err
		}
		defer database.Close()
		return fmt.Sprintf("ok (%s)", database.Path()), nil
	})

	// Project directories
	cfg, _ := config.Load()
	if cfg != nil {
		for _, p := range cfg.Projects {
			check(fmt.Sprintf("Project %s", p.Repo), func() (string, error) {
				if _, err := os.Stat(p.Path); os.IsNotExist(err) {
					return "", fmt.Errorf("path not found: %s", p.Path)
				}
				return fmt.Sprintf("ok (%s)", p.Path), nil
			})
		}
	}

	// Summary
	fmt.Printf("\n  %d passed, %d warnings, %d failed\n\n", passed, warned, failed)

	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	return nil
}
