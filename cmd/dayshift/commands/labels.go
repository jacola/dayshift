package commands

import (
	"fmt"
	"os"

	"github.com/marcus/dayshift/internal/config"
	gh "github.com/marcus/dayshift/internal/github"
	"github.com/spf13/cobra"
)

var labelsCmd = &cobra.Command{
	Use:   "labels",
	Short: "Manage dayshift labels",
}

var labelsSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Create all dayshift labels on configured repositories",
	Long: `Create the required GitHub labels for dayshift's pipeline on all
configured repositories (or a specific one with --repo).`,
	RunE: runLabelsSetup,
}

func init() {
	rootCmd.AddCommand(labelsCmd)
	labelsCmd.AddCommand(labelsSetupCmd)
	labelsSetupCmd.Flags().String("repo", "", "Target a specific repository (owner/repo)")
}

func runLabelsSetup(cmd *cobra.Command, args []string) error {
	repoFlag, _ := cmd.Flags().GetString("repo")

	cfg, err := config.Load()
	if err != nil {
		// Config may not exist yet; if --repo is given, proceed anyway
		if repoFlag == "" {
			return fmt.Errorf("load config: %w (run 'dayshift init' first)", err)
		}
		cfg = &config.Config{}
	}

	client := gh.NewClient()

	if !client.Available() {
		return fmt.Errorf("gh CLI not found in PATH — install from https://cli.github.com")
	}

	// Determine which repos to set up
	var repos []string
	if repoFlag != "" {
		repos = []string{repoFlag}
	} else {
		if len(cfg.Projects) == 0 {
			return fmt.Errorf("no projects configured — add projects to config or use --repo flag")
		}
		for _, p := range cfg.Projects {
			repos = append(repos, p.Repo)
		}
	}

	ctx := cmd.Context()
	hasError := false

	for _, repo := range repos {
		fmt.Printf("Setting up labels on %s...\n", repo)
		if err := client.EnsureLabels(ctx, repo); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Error: %v\n", err)
			hasError = true
			continue
		}
		fmt.Printf("  ✓ %d labels configured\n", len(gh.DayshiftLabels))
	}

	if hasError {
		return fmt.Errorf("some repositories had errors")
	}

	fmt.Println("\nDone! Labels are ready.")
	return nil
}
