package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/marcus/dayshift/internal/agents"
	"github.com/marcus/dayshift/internal/config"
	"github.com/marcus/dayshift/internal/db"
	gh "github.com/marcus/dayshift/internal/github"
	"github.com/marcus/dayshift/internal/pipeline"
	"github.com/marcus/dayshift/internal/scanner"
	"github.com/marcus/dayshift/internal/state"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Process issues (manual trigger)",
	Long:  `Process pending issues across configured projects. Can target a specific issue or phase.`,
	RunE:  runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().Int("issue", 0, "Process a specific issue number")
	runCmd.Flags().String("phase", "", "Run only a specific phase (research, plan, implement, validate)")
	runCmd.Flags().String("repo", "", "Process issues from a specific repo (owner/repo)")
	runCmd.Flags().String("provider", "", "Use a specific AI provider (claude, codex, copilot)")
	runCmd.Flags().Bool("dry-run", false, "Show what would be processed without executing")
	runCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}

func runRun(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	providerFlag, _ := cmd.Flags().GetString("provider")
	issueFlag, _ := cmd.Flags().GetInt("issue")
	repoFlag, _ := cmd.Flags().GetString("repo")

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w (run 'dayshift init' first)", err)
	}

	if len(cfg.Projects) == 0 {
		return fmt.Errorf("no projects configured — edit %s and add projects", config.GlobalConfigPath())
	}

	// Filter projects if --repo specified
	if repoFlag != "" {
		var filtered []config.ProjectConfig
		for _, p := range cfg.Projects {
			if p.Repo == repoFlag {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("repo %q not found in config", repoFlag)
		}
		cfg.Projects = filtered
	}

	// Open database
	database, err := db.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	// Create state manager
	stateMgr, err := state.New(database)
	if err != nil {
		return fmt.Errorf("create state manager: %w", err)
	}

	// Create GitHub client
	repoDirs := make(map[string]string)
	for _, p := range cfg.Projects {
		repoDirs[p.Repo] = p.Path
	}
	ghClient := gh.NewClient(gh.WithRepoDirs(repoDirs))
	if !ghClient.Available() {
		return fmt.Errorf("gh CLI not found — install from https://cli.github.com")
	}

	// Scan for work
	sc := scanner.New(ghClient, stateMgr, cfg)
	work, err := sc.Scan(cmd.Context())
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	// Filter by --issue if specified
	if issueFlag > 0 {
		var filtered []scanner.PendingWork
		for _, w := range work {
			if w.Issue.Number == issueFlag {
				filtered = append(filtered, w)
			}
		}
		work = filtered
	}

	// Display preflight
	if len(work) == 0 {
		fmt.Println("No pending issues to process.")
		return nil
	}

	fmt.Printf("\n📋 Dayshift — %d issue(s) to process\n\n", len(work))
	for i, w := range work {
		phaseArrow := fmt.Sprintf("→ %s", w.NextPhase)
		fmt.Printf("  %d. %s#%d: %s %s (%s)\n",
			i+1, w.Project.Repo, w.Issue.Number, w.Issue.Title, phaseArrow, w.Reason)
	}
	fmt.Println()

	if dryRun {
		fmt.Println("Dry run — no changes made.")
		return nil
	}

	// Confirm unless --yes
	if !yes {
		fmt.Print("Proceed? [Y/n]: ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response == "n" || response == "no" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Select provider
	agent, err := selectAgent(cfg, providerFlag)
	if err != nil {
		return fmt.Errorf("select provider: %w", err)
	}
	fmt.Printf("Using provider: %s\n\n", agent.Name())

	// Create executor
	executor := pipeline.NewExecutor(
		pipeline.WithAgent(agent),
		pipeline.WithGitHub(ghClient),
		pipeline.WithState(stateMgr),
		pipeline.WithConfig(cfg),
	)

	// Process issues
	start := time.Now()
	var processed, failed int

	for _, w := range work {
		fmt.Printf("▶ Processing %s#%d (%s)...\n", w.Project.Repo, w.Issue.Number, w.NextPhase)
		if err := executor.ProcessIssue(cmd.Context(), w); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Error: %v\n", err)
			failed++
		} else {
			fmt.Println("  ✓ Done")
			processed++
		}
	}

	// Summary
	elapsed := time.Since(start)
	fmt.Printf("\n✅ Complete: %d processed, %d failed (%s)\n", processed, failed, elapsed.Truncate(time.Second))

	// Record run
	stateMgr.RecordRun(&state.RunRecord{
		StartTime:       start,
		EndTime:         timePtr(time.Now()),
		Provider:        agent.Name(),
		IssuesProcessed: processed,
		Status:          runStatus(processed, failed),
	})

	return nil
}

func selectAgent(cfg *config.Config, providerFlag string) (agents.Agent, error) {
	preference := cfg.Provider.Preference
	if providerFlag != "" {
		preference = []string{providerFlag}
	}

	for _, name := range preference {
		switch name {
		case "claude":
			if cfg.Provider.Claude.Enabled {
				a := agents.NewClaudeAgent(
					agents.WithDangerouslySkipPermissions(cfg.Provider.Claude.DangerouslySkipPermissions),
				)
				if a.Available() {
					return a, nil
				}
			}
		case "codex":
			if cfg.Provider.Codex.Enabled {
				a := agents.NewCodexAgent(
					agents.WithDangerouslyBypassApprovalsAndSandbox(cfg.Provider.Codex.DangerouslyBypassApprovalsAndSandbox),
				)
				if a.Available() {
					return a, nil
				}
			}
		case "copilot":
			if cfg.Provider.Copilot.Enabled {
				a := agents.NewCopilotAgent()
				if a.Available() {
					return a, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no available provider found (tried: %s)", strings.Join(preference, ", "))
}

func timePtr(t time.Time) *time.Time { return &t }

func runStatus(processed, failed int) string {
	if failed == 0 {
		return "success"
	}
	if processed == 0 {
		return "failed"
	}
	return "partial"
}
