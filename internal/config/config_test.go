package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestDefaultConfig(t *testing.T) {
	cfg := &Config{}
	v := setupTestViper()
	setDefaults(v)
	if err := v.Unmarshal(cfg); err != nil {
		t.Fatalf("unmarshal defaults: %v", err)
	}

	if cfg.Schedule.PollInterval != "5m" {
		t.Errorf("expected poll_interval=5m, got %s", cfg.Schedule.PollInterval)
	}
	if cfg.Labels.Trigger != "dayshift" {
		t.Errorf("expected trigger=dayshift, got %s", cfg.Labels.Trigger)
	}
	if cfg.Labels.Prefix != "dayshift:" {
		t.Errorf("expected prefix=dayshift:, got %s", cfg.Labels.Prefix)
	}
	if cfg.Budget.MaxPercent != 100 {
		t.Errorf("expected max_percent=100, got %d", cfg.Budget.MaxPercent)
	}
	if !cfg.Phases.Research.Enabled {
		t.Error("expected phases.research.enabled=true")
	}
	if !cfg.Phases.Plan.Enabled {
		t.Error("expected phases.plan.enabled=true")
	}
	if cfg.Phases.Plan.MaxClarifyRounds != 3 {
		t.Errorf("expected max_clarify_rounds=3, got %d", cfg.Phases.Plan.MaxClarifyRounds)
	}
	if cfg.Phases.Approve.AutoApprove {
		t.Error("expected phases.approve.auto_approve=false")
	}
	if len(cfg.Provider.Preference) != 3 {
		t.Errorf("expected 3 providers, got %d", len(cfg.Provider.Preference))
	}
}

func TestValidateValidConfig(t *testing.T) {
	cfg := &Config{
		Projects: []ProjectConfig{
			{Repo: "owner/repo", Path: "/tmp/repo"},
		},
		Schedule: ScheduleConfig{PollInterval: "5m"},
		Labels:   LabelsConfig{Trigger: "dayshift", Prefix: "dayshift:"},
		Provider: ProviderConfig{
			Preference: []string{"claude"},
			Timeout:    "30m",
		},
		Budget: BudgetConfig{MaxPercent: 75},
		Phases: PhasesConfig{
			Plan: PhaseConfig{MaxClarifyRounds: 3},
		},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestValidateInvalidProvider(t *testing.T) {
	cfg := &Config{
		Provider: ProviderConfig{
			Preference: []string{"gpt5"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestValidateInvalidPollInterval(t *testing.T) {
	cfg := &Config{
		Schedule: ScheduleConfig{PollInterval: "not-a-duration"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid poll_interval")
	}
}

func TestValidateProjectRequiresRepoAndPath(t *testing.T) {
	tests := []struct {
		name    string
		project ProjectConfig
		wantErr bool
	}{
		{"valid", ProjectConfig{Repo: "owner/repo", Path: "/tmp"}, false},
		{"missing repo", ProjectConfig{Path: "/tmp"}, true},
		{"missing path", ProjectConfig{Repo: "owner/repo"}, true},
		{"bad repo format", ProjectConfig{Repo: "noslash", Path: "/tmp"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Projects: []ProjectConfig{tt.project}}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateBudgetRange(t *testing.T) {
	cfg := &Config{Budget: BudgetConfig{MaxPercent: 150}}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for max_percent > 100")
	}

	cfg.Budget.MaxPercent = -1
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for max_percent < 0")
	}
}

func TestPollIntervalDuration(t *testing.T) {
	cfg := &Config{Schedule: ScheduleConfig{PollInterval: "10m"}}
	if d := cfg.PollIntervalDuration(); d.Minutes() != 10 {
		t.Errorf("expected 10m, got %v", d)
	}

	cfg.Schedule.PollInterval = "invalid"
	if d := cfg.PollIntervalDuration(); d.Minutes() != 5 {
		t.Errorf("expected 5m fallback, got %v", d)
	}
}

func TestLoadFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `
projects:
  - repo: marcus/nightshift
    path: ~/code/nightshift
    priority: 1
  - repo: marcus/dayshift
    path: ~/code/dayshift
    priority: 2

schedule:
  poll_interval: 10m

labels:
  trigger: dayshift
  prefix: "dayshift:"

provider:
  preference: [claude, copilot]
  timeout: 45m

budget:
  max_percent: 80

phases:
  plan:
    max_clarify_rounds: 5
  approve:
    auto_approve: true

logging:
  level: debug
  format: text
`

	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadFromPath(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(cfg.Projects))
	}
	if cfg.Projects[0].Repo != "marcus/nightshift" {
		t.Errorf("expected repo=marcus/nightshift, got %s", cfg.Projects[0].Repo)
	}
	if cfg.Schedule.PollInterval != "10m" {
		t.Errorf("expected poll_interval=10m, got %s", cfg.Schedule.PollInterval)
	}
	if cfg.Budget.MaxPercent != 80 {
		t.Errorf("expected max_percent=80, got %d", cfg.Budget.MaxPercent)
	}
	if cfg.Phases.Plan.MaxClarifyRounds != 5 {
		t.Errorf("expected max_clarify_rounds=5, got %d", cfg.Phases.Plan.MaxClarifyRounds)
	}
	if !cfg.Phases.Approve.AutoApprove {
		t.Error("expected auto_approve=true")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("expected level=debug, got %s", cfg.Logging.Level)
	}
}

// setupTestViper creates a viper with defaults for testing.
func setupTestViper() *viper.Viper {
	v := viper.New()
	return v
}

// loadFromPath loads config from a specific file (for testing).
func loadFromPath(path string) (*Config, error) {
	v := viper.New()
	setDefaults(v)

	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.normalizePaths()
	return &cfg, nil
}
