// Package logging provides structured logging with file rotation for dayshift.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog with dayshift-specific functionality.
type Logger struct {
	zl        zerolog.Logger
	component string
	logDir    string
	file      *os.File
	mu        sync.Mutex
}

// Config holds logging configuration.
type Config struct {
	Level         string
	Path          string
	Format        string
	RetentionDays int
}

// DefaultConfig returns default logging configuration.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Level:         "info",
		Path:          filepath.Join(home, ".local", "share", "dayshift", "logs"),
		Format:        "json",
		RetentionDays: 7,
	}
}

var (
	globalLogger *Logger
	globalMu     sync.RWMutex
)

// Init initializes the global logger with the given configuration.
func Init(cfg Config) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	logger, err := New(cfg)
	if err != nil {
		return err
	}

	if globalLogger != nil && globalLogger.file != nil {
		_ = globalLogger.file.Close()
	}

	globalLogger = logger
	return nil
}

// New creates a new Logger instance.
func New(cfg Config) (*Logger, error) {
	if cfg.Level == "" {
		cfg.Level = "info"
	}
	if cfg.Format == "" {
		cfg.Format = "json"
	}
	if cfg.RetentionDays == 0 {
		cfg.RetentionDays = 7
	}

	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	if cfg.Path != "" {
		cfg.Path = expandPath(cfg.Path)
		if err := os.MkdirAll(cfg.Path, 0755); err != nil {
			return nil, fmt.Errorf("creating log dir: %w", err)
		}
	}

	logger := &Logger{logDir: cfg.Path}

	var writers []io.Writer

	if cfg.Path != "" {
		logFile := logger.currentLogPath()
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("opening log file: %w", err)
		}
		logger.file = f
		writers = append(writers, f)
		go logger.cleanOldLogs(cfg.RetentionDays)
	}

	var output io.Writer
	if len(writers) == 0 {
		output = os.Stderr
	} else {
		output = io.MultiWriter(writers...)
	}

	if cfg.Format == "text" {
		output = zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: time.RFC3339,
			NoColor:    true,
		}
	}

	logger.zl = zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Logger()

	return logger, nil
}

func (l *Logger) currentLogPath() string {
	filename := fmt.Sprintf("dayshift-%s.log", time.Now().Format("2006-01-02"))
	return filepath.Join(l.logDir, filename)
}

func (l *Logger) cleanOldLogs(retentionDays int) {
	if l.logDir == "" {
		return
	}
	entries, err := os.ReadDir(l.logDir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "dayshift-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		dateStr := strings.TrimPrefix(name, "dayshift-")
		dateStr = strings.TrimSuffix(dateStr, ".log")
		logDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if logDate.Before(cutoff) {
			_ = os.Remove(filepath.Join(l.logDir, name))
		}
	}
}

func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{
		zl:        l.zl.With().Str("component", component).Logger(),
		component: component,
		logDir:    l.logDir,
		file:      l.file,
	}
}

func (l *Logger) With() zerolog.Context { return l.zl.With() }
func (l *Logger) Debug(msg string)       { l.zl.Debug().Msg(msg) }
func (l *Logger) Info(msg string)        { l.zl.Info().Msg(msg) }
func (l *Logger) Warn(msg string)        { l.zl.Warn().Msg(msg) }
func (l *Logger) Error(msg string)       { l.zl.Error().Msg(msg) }

func (l *Logger) Debugf(format string, args ...any) { l.zl.Debug().Msgf(format, args...) }
func (l *Logger) Infof(format string, args ...any)  { l.zl.Info().Msgf(format, args...) }
func (l *Logger) Warnf(format string, args ...any)  { l.zl.Warn().Msgf(format, args...) }
func (l *Logger) Errorf(format string, args ...any) { l.zl.Error().Msgf(format, args...) }

func (l *Logger) InfoCtx(msg string, fields map[string]any) {
	event := l.zl.Info()
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)
}

func (l *Logger) ErrorCtx(msg string, fields map[string]any) {
	event := l.zl.Error()
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)
}

func (l *Logger) Err(err error) *zerolog.Event { return l.zl.Error().Err(err) }

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

func (l *Logger) LogFiles() ([]string, error) {
	if l.logDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(l.logDir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "dayshift-") && strings.HasSuffix(name, ".log") {
			files = append(files, filepath.Join(l.logDir, name))
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i] > files[j] })
	return files, nil
}

// Get returns the global logger.
func Get() *Logger {
	globalMu.RLock()
	defer globalMu.RUnlock()
	if globalLogger == nil {
		return &Logger{
			zl: zerolog.New(os.Stderr).With().Timestamp().Logger(),
		}
	}
	return globalLogger
}

// Component returns a logger with the specified component.
func Component(name string) *Logger { return Get().WithComponent(name) }

func parseLevel(level string) (zerolog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return zerolog.DebugLevel, nil
	case "info":
		return zerolog.InfoLevel, nil
	case "warn":
		return zerolog.WarnLevel, nil
	case "error":
		return zerolog.ErrorLevel, nil
	default:
		return zerolog.InfoLevel, fmt.Errorf("invalid log level: %s", level)
	}
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
