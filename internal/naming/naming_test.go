package naming

import (
	"strings"
	"testing"
	"time"
)

func TestDeriveTimestamp(t *testing.T) {
	received := []string{
		"by mailbox.example.com; Sat, 12 Sep 2026 00:01:00 +0000",
		"from mail.test; Fri, 11 Sep 2026 23:55:00 -0400",
	}
	dateHeader := "Fri, 11 Sep 2026 12:00:00 -0500"

	// Received header takes precedence (first usable hop)
	when, _, ok := DeriveTimestamp(received, dateHeader)
	if !ok {
		t.Fatal("expected timestamp to be found")
	}
	if when.Format(time.RFC3339) != "2026-09-12T00:01:00Z" {
		t.Fatalf("unexpected time: %s", when.Format(time.RFC3339))
	}

	// When Received is empty or malformed, fallback to Date
	when, _, ok = DeriveTimestamp(nil, dateHeader)
	if !ok {
		t.Fatal("expected timestamp from Date")
	}
	if when.Format("2006-01-02") != "2026-09-11" {
		t.Fatalf("unexpected date from Date header: %s", when.Format("2006-01-02"))
	}

	// When both empty, undated
	_, _, ok = DeriveTimestamp(nil, "")
	if ok {
		t.Fatal("expected no timestamp for empty inputs")
	}
}

func TestSlugifySubject(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"Recent Canvas Notifications", "recent-canvas-notifications"},
		{"**WEEKLY ANNOUNCEMENT** Sep 11", "weekly-announcement-sep-11"},
		{"Re: recent CANVAS notifications", "re-recent-canvas-notifications"},
		{"Very Long Subject " + strings.Repeat("ABC ", 20), "very-long-subject-abc-abc-abc-abc-abc-abc-abc-abc-abc-abc-abc-abc-abc"},
		{"   ", "message"},
		{"$$$Special @# Characters!!", "special-characters"},
	}
	for _, c := range cases {
		got := SlugifySubject(c.input)
		if got != c.expected {
			t.Errorf("SlugifySubject(%q) = %q, expected %q", c.input, got, c.expected)
		}
		if len(got) > 70 {
			t.Errorf("slug exceeds 70 chars: %q", got)
		}
	}
}

func TestFormatFilenames(t *testing.T) {
	when := time.Date(2026, 9, 12, 0, 1, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)

	dateName := FormatDateFilename(when, true, "Recent Canvas Notifications", hash)
	if dateName != "2026-09-12-recent-canvas-notifications-aaaaaaaaaaaa.md" {
		t.Fatalf("unexpected date filename: %s", dateName)
	}

	tsName := FormatTimestampFilename(when, true, "Recent Canvas Notifications", hash)
	if tsName != "2026-09-12_00-01-00Z-recent-canvas-notifications-aaaaaaaaaaaa.md" {
		t.Fatalf("unexpected timestamp filename: %s", tsName)
	}

	undatedName := FormatDateFilename(time.Time{}, false, "Recent Canvas Notifications", hash)
	if undatedName != "undated-recent-canvas-notifications-aaaaaaaaaaaa.md" {
		t.Fatalf("unexpected undated filename: %s", undatedName)
	}
}
