package commands

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/marcus/dayshift/internal/db"
	"github.com/marcus/dayshift/internal/state"
	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset <issue-url-or-number>",
	Short: "Reset an issue to restart processing from scratch",
	Long: `Reset a tracked issue so dayshift will reprocess it from the beginning.

Accepts a GitHub issue URL or just an issue number (with --repo).

Examples:
  dayshift reset https://github.com/owner/repo/issues/42
  dayshift reset 42 --repo owner/repo`,
	Args: cobra.ExactArgs(1),
	RunE: runReset,
}

func init() {
	rootCmd.AddCommand(resetCmd)
	resetCmd.Flags().String("repo", "", "Repository (owner/repo) when using issue number")
}

var issueURLPattern = regexp.MustCompile(`github\.com/([^/]+/[^/]+)/issues/(\d+)`)

func runReset(cmd *cobra.Command, args []string) error {
	repoFlag, _ := cmd.Flags().GetString("repo")

	var repo string
	var number int

	// Parse argument — URL or number
	if matches := issueURLPattern.FindStringSubmatch(args[0]); len(matches) == 3 {
		repo = matches[1]
		n, _ := strconv.Atoi(matches[2])
		number = n
	} else {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid argument %q — provide a GitHub issue URL or number", args[0])
		}
		number = n
		repo = repoFlag
		if repo == "" {
			return fmt.Errorf("specify --repo when using an issue number")
		}
	}

	database, err := db.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	stateMgr, err := state.New(database)
	if err != nil {
		return fmt.Errorf("create state manager: %w", err)
	}

	issue, err := stateMgr.GetIssue(repo, number)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}
	if issue == nil {
		fmt.Printf("Issue %s#%d is not tracked by dayshift.\n", repo, number)
		return nil
	}

	// Delete all related records
	sqlDB := database.SQL()
	tx, err := sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	queries := []string{
		"DELETE FROM issue_comments WHERE issue_id = ?",
		"DELETE FROM phase_history WHERE issue_id = ?",
		"DELETE FROM issues WHERE id = ?",
	}
	for _, q := range queries {
		if _, err := tx.Exec(q, issue.ID); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("reset: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	fmt.Printf("✓ Reset %s#%d — removed from tracking (was in %s phase)\n", repo, number, issue.Phase)
	fmt.Println("  The issue will be picked up fresh on the next run if it still has the trigger label.")

	// Clean up dayshift labels
	fmt.Println("\n  To also remove dayshift labels from the issue:")
	fmt.Printf("  gh issue edit %d -R %s", number, repo)
	labels := []string{"dayshift:researched", "dayshift:planned", "dayshift:needs-input",
		"dayshift:awaiting-approval", "dayshift:approved", "dayshift:implementing",
		"dayshift:implemented", "dayshift:validated", "dayshift:needs-fixes",
		"dayshift:complete", "dayshift:error", "dayshift:paused"}
	for _, l := range labels {
		fmt.Printf(" --remove-label %q", l)
	}
	fmt.Println()

	return nil
}
