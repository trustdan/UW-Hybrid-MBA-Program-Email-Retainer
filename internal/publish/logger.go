package publish

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var sensitiveQueryParamRegex = regexp.MustCompile(`(?i)(code|token|access_token|refresh_token|sig|signature|key|secret)=[^&\s]+`)

// SanitizeLogMessage scrubs potential secrets like tokens, keys, and credentials from log messages.
func SanitizeLogMessage(msg string) string {
	return sensitiveQueryParamRegex.ReplaceAllString(msg, "$1=[REDACTED]")
}

// Logger writes sanitized, rotating private run logs to stateDir/logs.
type Logger struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	logsDir string
}

// NewLogger creates a new log file in stateDir/logs and rotates out old logs.
func NewLogger(stateDir string, maxKept int) (*Logger, error) {
	logsDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logsDir, 0700); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	// Rotate before creating the new log
	if maxKept > 0 {
		rotateLogs(logsDir, maxKept-1)
	}

	filename := fmt.Sprintf("run-%s.log", time.Now().UTC().Format("2006-01-02-150405"))
	logPath := filepath.Join(logsDir, filename)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", logPath, err)
	}

	l := &Logger{
		file:    f,
		path:    logPath,
		logsDir: logsDir,
	}
	l.Logf("Logger initialized (path=%s)", logPath)
	return l, nil
}

// Logf writes a timestamped, sanitized log line.
func (l *Logger) Logf(format string, args ...any) {
	if l == nil || l.file == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	raw := fmt.Sprintf(format, args...)
	sanitized := SanitizeLogMessage(raw)
	timestamp := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("[%s] %s\n", timestamp, strings.TrimRight(sanitized, "\r\n"))
	_, _ = l.file.WriteString(line)
}

// Close closes the underlying log file.
func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.file.Close()
	l.file = nil
	return err
}

// Path returns the path of the current log file.
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// rotateLogs keeps only the most recent keepCount log files.
func rotateLogs(logsDir string, keepCount int) {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return
	}

	var logFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "run-") && strings.HasSuffix(e.Name(), ".log") {
			logFiles = append(logFiles, e.Name())
		}
	}

	if len(logFiles) <= keepCount {
		return
	}

	sort.Strings(logFiles)
	toDelete := len(logFiles) - keepCount
	for i := 0; i < toDelete; i++ {
		_ = os.Remove(filepath.Join(logsDir, logFiles[i]))
	}
}
