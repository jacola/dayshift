// Package config handles loading and validating dayshift configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is the root configuration struct for dayshift.
type Config struct {
	Projects []ProjectConfig `mapstructure:"projects"`
	Schedule ScheduleConfig  `mapstructure:"schedule"`
	Labels   LabelsConfig    `mapstructure:"labels"`
	Provider ProviderConfig  `mapstructure:"provider"`
	Budget   BudgetConfig    `mapstructure:"budget"`
	Phases   PhasesConfig    `mapstructure:"phases"`
	Logging  LoggingConfig   `mapstructure:"logging"`
}

// ProjectConfig defines a single project (GitHub repo + local checkout).
type ProjectConfig struct {
	Repo     string `mapstructure:"repo"`
	Path     string `mapstructure:"path"`
	Priority int    `mapstructure:"priority"`
}

// ScheduleConfig controls polling frequency.
type ScheduleConfig struct {
	PollInterval string `mapstructure:"poll_interval"`
}

// LabelsConfig controls label naming.
type LabelsConfig struct {
	Trigger string `mapstructure:"trigger"`
	Prefix  string `mapstructure:"prefix"`
}

// ProviderConfig controls AI provider selection.
type ProviderConfig struct {
	Preference []string             `mapstructure:"preference"`
	Claude     ProviderDetailConfig `mapstructure:"claude"`
	Codex      ProviderDetailConfig `mapstructure:"codex"`
	Copilot    ProviderDetailConfig `mapstructure:"copilot"`
	Timeout    string               `mapstructure:"timeout"`
}

// ProviderDetailConfig holds per-provider settings.
type ProviderDetailConfig struct {
	Enabled                              bool   `mapstructure:"enabled"`
	DataPath                             string `mapstructure:"data_path"`
	DangerouslySkipPermissions           bool   `mapstructure:"dangerously_skip_permissions"`
	DangerouslyBypassApprovalsAndSandbox bool   `mapstructure:"dangerously_bypass_approvals_and_sandbox"`
}

// BudgetConfig controls token budget limits.
type BudgetConfig struct {
	Mode       string `mapstructure:"mode"`
	MaxPercent int    `mapstructure:"max_percent"`
}

// PhasesConfig controls which pipeline phases are enabled.
type PhasesConfig struct {
	Research  PhaseConfig `mapstructure:"research"`
	Plan      PhaseConfig `mapstructure:"plan"`
	Approve   PhaseConfig `mapstructure:"approve"`
	Implement PhaseConfig `mapstructure:"implement"`
	Validate  PhaseConfig `mapstructure:"validate"`
}

// PhaseConfig holds per-phase settings.
type PhaseConfig struct {
	Enabled          bool `mapstructure:"enabled"`
	MaxClarifyRounds int  `mapstructure:"max_clarify_rounds"`
	AutoApprove      bool `mapstructure:"auto_approve"`
}

// LoggingConfig controls log output.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Path   string `mapstructure:"path"`
	Format string `mapstructure:"format"`
}

// GlobalConfigDir returns the directory for global config.
func GlobalConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "dayshift")
}

// GlobalConfigPath returns the default global config file path.
func GlobalConfigPath() string {
	return filepath.Join(GlobalConfigDir(), "config.yaml")
}

// Load reads configuration from the global config file and applies defaults.
func Load() (*Config, error) {
	v := viper.New()
	setDefaults(v)

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(GlobalConfigDir())

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	bindEnvVars(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	cfg.normalizePaths()

	return &cfg, nil
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.Schedule.PollInterval != "" {
		if _, err := time.ParseDuration(c.Schedule.PollInterval); err != nil {
			return fmt.Errorf("invalid poll_interval %q: %w", c.Schedule.PollInterval, err)
		}
	}

	if c.Budget.MaxPercent < 0 || c.Budget.MaxPercent > 100 {
		return fmt.Errorf("budget.max_percent must be 0-100, got %d", c.Budget.MaxPercent)
	}

	if c.Provider.Timeout != "" {
		if _, err := time.ParseDuration(c.Provider.Timeout); err != nil {
			return fmt.Errorf("invalid provider.timeout %q: %w", c.Provider.Timeout, err)
		}
	}

	for _, p := range c.Provider.Preference {
		switch p {
		case "claude", "codex", "copilot":
		default:
			return fmt.Errorf("unknown provider %q in preference list", p)
		}
	}

	for i, proj := range c.Projects {
		if proj.Repo == "" {
			return fmt.Errorf("projects[%d].repo is required", i)
		}
		if proj.Path == "" {
			return fmt.Errorf("projects[%d].path is required", i)
		}
		if !strings.Contains(proj.Repo, "/") {
			return fmt.Errorf("projects[%d].repo must be in owner/repo format, got %q", i, proj.Repo)
		}
	}

	if c.Phases.Plan.MaxClarifyRounds < 0 {
		return fmt.Errorf("phases.plan.max_clarify_rounds must be >= 0")
	}

	switch c.Logging.Level {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid logging.level: %q", c.Logging.Level)
	}

	switch c.Logging.Format {
	case "", "json", "text":
	default:
		return fmt.Errorf("invalid logging.format: %q", c.Logging.Format)
	}

	return nil
}

func (c *Config) normalizePaths() {
	for i := range c.Projects {
		c.Projects[i].Path = expandPath(c.Projects[i].Path)
	}
	if c.Logging.Path != "" {
		c.Logging.Path = expandPath(c.Logging.Path)
	}
}

// PollIntervalDuration returns the poll interval as a time.Duration.
func (c *Config) PollIntervalDuration() time.Duration {
	d, err := time.ParseDuration(c.Schedule.PollInterval)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// ProviderTimeout returns the provider timeout as a time.Duration.
func (c *Config) ProviderTimeout() time.Duration {
	d, err := time.ParseDuration(c.Provider.Timeout)
	if err != nil {
		return 30 * time.Minute
	}
	return d
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("schedule.poll_interval", "5m")

	v.SetDefault("labels.trigger", "dayshift")
	v.SetDefault("labels.prefix", "dayshift:")

	v.SetDefault("provider.preference", []string{"claude", "copilot", "codex"})
	v.SetDefault("provider.timeout", "30m")
	v.SetDefault("provider.claude.enabled", true)
	v.SetDefault("provider.claude.data_path", "~/.claude")
	v.SetDefault("provider.claude.dangerously_skip_permissions", true)
	v.SetDefault("provider.codex.enabled", true)
	v.SetDefault("provider.codex.data_path", "~/.codex")
	v.SetDefault("provider.codex.dangerously_bypass_approvals_and_sandbox", true)
	v.SetDefault("provider.copilot.enabled", true)
	v.SetDefault("provider.copilot.data_path", "~/.copilot")

	v.SetDefault("budget.mode", "daily")
	v.SetDefault("budget.max_percent", 100)

	v.SetDefault("phases.research.enabled", true)
	v.SetDefault("phases.plan.enabled", true)
	v.SetDefault("phases.plan.max_clarify_rounds", 3)
	v.SetDefault("phases.approve.enabled", true)
	v.SetDefault("phases.approve.auto_approve", false)
	v.SetDefault("phases.implement.enabled", true)
	v.SetDefault("phases.validate.enabled", true)

	home, _ := os.UserHomeDir()
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.path", filepath.Join(home, ".local", "share", "dayshift", "logs"))
	v.SetDefault("logging.format", "json")
}

func bindEnvVars(v *viper.Viper) {
	v.SetEnvPrefix("DAYSHIFT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
}

func expandPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
