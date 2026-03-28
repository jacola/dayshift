package commands

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	Version = "0.1.0"
)

var rootCmd = &cobra.Command{
	Use:   "dayshift",
	Short: "Issue-driven AI agent pipeline",
	Long: `Dayshift processes GitHub issues through a structured pipeline:
Research → Plan → Approve → Implement → Validate.

It pauses for human input when needed and resumes autonomously
when humans respond via issue comments and labels.`,
	Version: Version,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().Bool("verbose", false, "Enable verbose output")
}
