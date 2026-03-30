package commands

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcus/dayshift/internal/config"
	"github.com/spf13/cobra"
)

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a dayshift configuration file",
	Long: `Initialize a new dayshift configuration file.

Creates a global config at ~/.config/dayshift/config.yaml with
sensible defaults and helpful comments.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolP("force", "f", false, "Overwrite existing config without prompting")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	configPath := config.GlobalConfigPath()

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		if !force {
			fmt.Printf("%sConfig already exists:%s %s\n", colorYellow, colorReset, configPath)
			fmt.Print("Overwrite? [y/N]: ")
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "y" && response != "yes" {
				fmt.Println("Aborted.")
				return nil
			}
		}
	}

	// Create parent directory if needed
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	// Write config
	content := generateDefaultConfig()
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("\n%s%sCreated config:%s %s\n\n", colorBold, colorGreen, colorReset, configPath)
	fmt.Printf("%sNext steps:%s\n", colorCyan, colorReset)
	fmt.Println("  1. Edit the config to add your project repositories")
	fmt.Println("  2. Run 'dayshift doctor' to verify your setup")
	fmt.Println("  3. Run 'dayshift labels setup' to create labels on your repos")
	fmt.Println("  4. Run 'dayshift run --dry-run' to preview")
	fmt.Println()

	return nil
}

func generateDefaultConfig() string {
	return `# Dayshift Configuration
# Location: ~/.config/dayshift/config.yaml
#
# Dayshift processes GitHub issues through a structured pipeline:
# Research → Plan → Implement → Validate
#
# It pauses for human input when needed and resumes autonomously
# when humans respond via issue comments and labels.

# Projects to monitor
# Each project maps a GitHub repo to a local checkout path.
projects:
  # - repo: owner/repo            # GitHub repository (owner/repo)
  #   path: ~/code/repo            # Local checkout path
  #   priority: 1                  # Higher = processed first

# Schedule configuration
schedule:
  poll_interval: 5m              # How often to check for issue changes

# Label configuration
labels:
  trigger: dayshift              # Label that activates processing
  prefix: "dayshift:"            # Prefix for phase labels

# AI provider configuration
provider:
  preference:                    # Provider preference order
    - claude
    - copilot
    - codex
  timeout: 30m                   # Per-phase execution timeout
  claude:
    enabled: true
    data_path: "~/.claude"
    dangerously_skip_permissions: true
  codex:
    enabled: true
    data_path: "~/.codex"
    dangerously_bypass_approvals_and_sandbox: true
  copilot:
    enabled: true
    data_path: "~/.copilot"

# Budget configuration
budget:
  mode: daily                    # daily | weekly
  max_percent: 100               # Max % of budget per run

# Pipeline phase configuration
phases:
  research:
    enabled: true                # Research the codebase for issue context
  plan:
    enabled: true                # Create an implementation plan
    max_clarify_rounds: 3        # Max Q&A iterations before escalating
  implement:
    enabled: true                # Implement the plan
  validate:
    enabled: true                # Validate implementation against plan

# Logging configuration
logging:
  level: info                    # debug | info | warn | error
  path: ~/.local/share/dayshift/logs
  format: json                   # json | text
`
}
