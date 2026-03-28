package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/marcus/dayshift/internal/config"
	"github.com/marcus/dayshift/internal/db"
	gh "github.com/marcus/dayshift/internal/github"
	"github.com/marcus/dayshift/internal/logging"
	"github.com/marcus/dayshift/internal/pipeline"
	"github.com/marcus/dayshift/internal/scanner"
	"github.com/marcus/dayshift/internal/state"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the background polling daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the background daemon",
	RunE:  runDaemonStart,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background daemon",
	RunE:  runDaemonStop,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE:  runDaemonStatus,
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonStartCmd.Flags().BoolP("foreground", "f", false, "Run in foreground")
}

func pidFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "dayshift", "dayshift.pid")
}

func writePidFile() error {
	path := pidFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func removePidFile() {
	os.Remove(pidFilePath())
}

func readPid() (int, error) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func isDaemonRunning() (bool, int) {
	pid, err := readPid()
	if err != nil {
		return false, 0
	}
	return isProcessRunning(pid), pid
}

func runDaemonStart(cmd *cobra.Command, args []string) error {
	foreground, _ := cmd.Flags().GetBool("foreground")

	if running, pid := isDaemonRunning(); running {
		return fmt.Errorf("daemon already running (PID %d)", pid)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.Projects) == 0 {
		return fmt.Errorf("no projects configured")
	}

	if foreground {
		return runDaemonLoop(cfg)
	}

	// Launch in background
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable: %w", err)
	}

	bgCmd := &exec.Cmd{
		Path:        exe,
		Args:        []string{exe, "daemon", "start", "--foreground"},
		SysProcAttr: &syscall.SysProcAttr{Setsid: true},
	}

	if err := bgCmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	fmt.Printf("Daemon started (PID %d)\n", bgCmd.Process.Pid)
	return nil
}

func runDaemonLoop(cfg *config.Config) error {
	logCfg := logging.Config{
		Level:  cfg.Logging.Level,
		Path:   cfg.Logging.Path,
		Format: cfg.Logging.Format,
	}
	if err := logging.Init(logCfg); err != nil {
		return fmt.Errorf("init logging: %w", err)
	}

	logger := logging.Component("daemon")
	logger.Info("daemon starting")

	if err := writePidFile(); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer removePidFile()

	database, err := db.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Infof("received signal: %v", sig)
		cancel()
	}()

	pollInterval := cfg.PollIntervalDuration()
	logger.Infof("polling every %s for %d project(s)", pollInterval, len(cfg.Projects))

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Run immediately on start
	runScheduledCheck(ctx, cfg, database, logger)

	for {
		select {
		case <-ctx.Done():
			logger.Info("daemon shutting down")
			return nil
		case <-ticker.C:
			runScheduledCheck(ctx, cfg, database, logger)
		}
	}
}

func runScheduledCheck(ctx context.Context, cfg *config.Config, database *db.DB, logger *logging.Logger) {
	logger.Info("starting scheduled check")

	stateMgr, err := state.New(database)
	if err != nil {
		logger.Errorf("create state manager: %v", err)
		return
	}

	repoDirs := make(map[string]string)
	for _, p := range cfg.Projects {
		repoDirs[p.Repo] = p.Path
	}
	ghClient := gh.NewClient(gh.WithRepoDirs(repoDirs))

	triggerLabel := cfg.Labels.Trigger
	if triggerLabel == "" {
		triggerLabel = "dayshift"
	}
	if err := stateMgr.Reconcile(ctx, ghClient, cfg.Projects, triggerLabel); err != nil {
		logger.Errorf("reconcile: %v", err)
	}

	sc := scanner.New(ghClient, stateMgr, cfg)
	work, err := sc.Scan(ctx)
	if err != nil {
		logger.Errorf("scan: %v", err)
		return
	}

	if len(work) == 0 {
		logger.Debug("no pending work")
		return
	}

	logger.Infof("found %d issue(s) to process", len(work))

	agent, err := selectAgent(cfg, "")
	if err != nil {
		logger.Errorf("select agent: %v", err)
		return
	}

	executor := pipeline.NewExecutor(
		pipeline.WithAgent(agent),
		pipeline.WithGitHub(ghClient),
		pipeline.WithState(stateMgr),
		pipeline.WithConfig(cfg),
	)

	start := time.Now()
	var processed, failed int

	for _, w := range work {
		if ctx.Err() != nil {
			break
		}
		logger.Infof("processing %s#%d (%s)", w.Project.Repo, w.Issue.Number, w.NextPhase)
		if err := executor.ProcessIssue(ctx, w); err != nil {
			logger.Errorf("process %s#%d: %v", w.Project.Repo, w.Issue.Number, err)
			failed++
		} else {
			processed++
		}
	}

	elapsed := time.Since(start)
	logger.Infof("check complete: %d processed, %d failed (%s)", processed, failed, elapsed.Truncate(time.Second))

	now := time.Now()
	if err := stateMgr.RecordRun(&state.RunRecord{
		StartTime:       start,
		EndTime:         &now,
		Provider:        agent.Name(),
		IssuesProcessed: processed,
		Status:          runStatus(processed, failed),
	}); err != nil {
		logger.Errorf("record run: %v", err)
	}
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	running, pid := isDaemonRunning()
	if !running {
		if pid > 0 {
			removePidFile()
			fmt.Println("Removed stale PID file.")
		} else {
			fmt.Println("Daemon is not running.")
		}
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	// Wait for graceful shutdown
	for i := 0; i < 100; i++ {
		time.Sleep(100 * time.Millisecond)
		if !isProcessRunning(pid) {
			fmt.Println("Daemon stopped.")
			removePidFile()
			return nil
		}
	}

	if err := process.Signal(syscall.SIGKILL); err != nil {
		return fmt.Errorf("send SIGKILL: %w", err)
	}

	removePidFile()
	fmt.Println("Daemon forcefully stopped.")
	return nil
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	running, pid := isDaemonRunning()
	if running {
		fmt.Printf("Daemon is running (PID %d)\n", pid)
	} else {
		fmt.Println("Daemon is not running.")
	}
	return nil
}
