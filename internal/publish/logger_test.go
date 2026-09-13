package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeLogMessage(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "fetching https://graph.microsoft.com/v1.0/me?access_token=secret123&code=auth456",
			expected: "fetching https://graph.microsoft.com/v1.0/me?access_token=[REDACTED]&code=[REDACTED]",
		},
		{
			input:    "processing email from classmate@uw.edu with subject Welcome",
			expected: "processing email from classmate@uw.edu with subject Welcome",
		},
	}

	for _, tt := range tests {
		got := SanitizeLogMessage(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeLogMessage(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestLogger_Rotation(t *testing.T) {
	tempDir := t.TempDir()

	// Create 5 dummy logs
	logsDir := filepath.Join(tempDir, "logs")
	_ = os.MkdirAll(logsDir, 0700)
	for i := 1; i <= 5; i++ {
		name := filepath.Join(logsDir, strings.ReplaceAll("run-2026-09-01-100000.log", "100000", string(rune('0'+i))+"00000"))
		_ = os.WriteFile(name, []byte("log"), 0600)
	}

	// Create new logger with maxKept = 3
	logger, err := NewLogger(tempDir, 3)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.Logf("test entry token=xyz")

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) > 3 {
		t.Errorf("expected at most 3 log files after rotation, got %d", len(entries))
	}

	// Verify content of current log has sanitized entry
	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile log failed: %v", err)
	}
	if strings.Contains(string(data), "xyz") {
		t.Errorf("expected secret 'xyz' to be redacted")
	}
	if !strings.Contains(string(data), "token=[REDACTED]") {
		t.Errorf("expected token=[REDACTED] in log")
	}
}
