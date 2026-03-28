package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/marcus/dayshift/internal/config"
	"github.com/marcus/dayshift/internal/db"
	"github.com/marcus/dayshift/internal/state"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show issue processing status",
	Long:  `Display the current status of tracked issues across all configured projects.`,
	RunE:  runStatusCmd,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().Int("issue", 0, "Show detailed status for a specific issue")
	statusCmd.Flags().String("repo", "", "Filter by repository (owner/repo)")
}

func runStatusCmd(cmd *cobra.Command, args []string) error {
	issueFlag, _ := cmd.Flags().GetInt("issue")
	repoFlag, _ := cmd.Flags().GetString("repo")

	database, err := db.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	stateMgr, err := state.New(database)
	if err != nil {
		return fmt.Errorf("create state manager: %w", err)
	}

	// Specific issue detail
	if issueFlag > 0 {
		repo := repoFlag
		if repo == "" {
			cfg, _ := config.Load()
			if cfg != nil && len(cfg.Projects) > 0 {
				repo = cfg.Projects[0].Repo
			}
		}
		if repo == "" {
			return fmt.Errorf("specify --repo when using --issue")
		}
		return showIssueDetail(stateMgr, repo, issueFlag)
	}

	// List all issues
	issues, err := stateMgr.ListAllIssues()
	if err != nil {
		return fmt.Errorf("list issues: %w", err)
	}

	if len(issues) == 0 {
		fmt.Println("No tracked issues.")
		return nil
	}

	// Filter by repo
	if repoFlag != "" {
		var filtered []state.IssueState
		for _, issue := range issues {
			if issue.Repo == repoFlag {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	fmt.Printf("\n📊 Dayshift Status — %d tracked issue(s)\n\n", len(issues))
	fmt.Printf("  %-20s %-6s %-30s %-12s %s\n", "REPO", "ISSUE", "TITLE", "PHASE", "AGE")
	fmt.Printf("  %s\n", strings.Repeat("─", 90))

	for _, issue := range issues {
		title := issue.Title
		if len(title) > 28 {
			title = title[:28] + "…"
		}
		age := formatAge(issue.UpdatedAt)
		phase := formatPhase(issue.Phase)

		fmt.Printf("  %-20s #%-5d %-30s %-12s %s\n",
			truncRepo(issue.Repo), issue.IssueNumber, title, phase, age)
	}

	// Recent runs
	runs, _ := stateMgr.GetRecentRuns(5)
	if len(runs) > 0 {
		fmt.Printf("\n📈 Recent Runs\n\n")
		for _, run := range runs {
			elapsed := ""
			if run.EndTime != nil {
				elapsed = run.EndTime.Sub(run.StartTime).Truncate(time.Second).String()
			}
			fmt.Printf("  %s  %-8s  %d issues  %s  %s\n",
				run.StartTime.Format("Jan 02 15:04"),
				run.Provider, run.IssuesProcessed, run.Status, elapsed)
		}
	}

	fmt.Println()
	return nil
}

func showIssueDetail(mgr *state.Manager, repo string, number int) error {
	issue, err := mgr.GetIssue(repo, number)
	if err != nil {
		return err
	}
	if issue == nil {
		fmt.Printf("Issue %s#%d is not tracked by dayshift.\n", repo, number)
		return nil
	}

	fmt.Printf("\n📋 %s#%d: %s\n\n", issue.Repo, issue.IssueNumber, issue.Title)
	fmt.Printf("  Phase:   %s\n", formatPhase(issue.Phase))
	if issue.PhaseStarted != nil {
		fmt.Printf("  Started: %s (%s ago)\n", issue.PhaseStarted.Format("Jan 02 15:04"), formatAge(*issue.PhaseStarted))
	}
	if issue.PRURL != "" {
		fmt.Printf("  PR:      %s\n", issue.PRURL)
	}
	fmt.Printf("  Created: %s\n", issue.CreatedAt.Format("Jan 02 15:04"))
	fmt.Printf("  Updated: %s\n", issue.UpdatedAt.Format("Jan 02 15:04"))
	fmt.Println()

	return nil
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func formatPhase(phase string) string {
	symbols := map[string]string{
		"pending":   "⏳ pending",
		"research":  "🔍 research",
		"plan":      "📝 plan",
		"clarify":   "❓ clarify",
		"approve":   "👀 approve",
		"implement": "🔨 implement",
		"validate":  "✅ validate",
		"complete":  "🎉 complete",
		"error":     "❌ error",
		"paused":    "⏸️  paused",
	}
	if s, ok := symbols[phase]; ok {
		return s
	}
	return phase
}

func truncRepo(repo string) string {
	if len(repo) > 18 {
		return repo[:18] + "…"
	}
	return repo
}
