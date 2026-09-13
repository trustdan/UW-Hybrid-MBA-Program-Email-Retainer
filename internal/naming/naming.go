// Package naming derives delivery timestamps, subject slugs, and stable filenames.
package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

var (
	nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)
)

// ParseEmailDate attempts to parse an RFC 5322 / RFC 822 date string.
func ParseEmailDate(dateStr string) (time.Time, bool) {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return time.Time{}, false
	}
	// Try standard mail.ParseDate
	t, err := mail.ParseDate(dateStr)
	if err == nil {
		return t.UTC(), true
	}
	// Try other common email date formats
	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 MST",
		"2 Jan 2006 15:04:05 -0700",
		"2 Jan 2006 15:04:05 MST",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, dateStr); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// DeriveTimestamp inspects Received headers first (last hop delivery), then Date header.
// Returns the parsed UTC time, a boolean indicating if a timestamp was found, and whether it has precise time.
func DeriveTimestamp(received []string, dateHeader string) (t time.Time, hasTime bool, found bool) {
	// First usable Received header is the last hop
	for _, r := range received {
		parts := strings.Split(r, ";")
		if len(parts) >= 2 {
			candidate := strings.TrimSpace(parts[len(parts)-1])
			if parsed, ok := ParseEmailDate(candidate); ok {
				return parsed, true, true
			}
		}
	}
	// Fallback to sender's Date header
	if parsed, ok := ParseEmailDate(dateHeader); ok {
		return parsed, true, true
	}
	return time.Time{}, false, false
}

// SlugifySubject converts subject to safe lowercase hyphenated slug, max 70 chars.
func SlugifySubject(subject string) string {
	clean := strings.ToLower(subject)
	clean = nonAlphanumeric.ReplaceAllString(clean, "-")
	clean = strings.Trim(clean, "-")
	if len(clean) > 70 {
		clean = strings.TrimRight(clean[:70], "-")
	}
	if clean == "" {
		clean = "message"
	}
	return clean
}

// FormatDateFilename produces legacy-compatible YYYY-MM-DD-subject-<12-hash>.md
// or undated-subject-<12-hash>.md.
func FormatDateFilename(t time.Time, found bool, subject, sha256Hex string) string {
	slug := SlugifySubject(subject)
	shortHash := ShortHash(sha256Hex)
	if !found {
		return fmt.Sprintf("undated-%s-%s.md", slug, shortHash)
	}
	dateStr := t.UTC().Format("2006-01-02")
	return fmt.Sprintf("%s-%s-%s.md", dateStr, slug, shortHash)
}

// FormatTimestampFilename produces YYYY-MM-DD_HH-MM-SSZ-subject-<12-hash>.md per B05.
func FormatTimestampFilename(t time.Time, found bool, subject, sha256Hex string) string {
	slug := SlugifySubject(subject)
	shortHash := ShortHash(sha256Hex)
	if !found {
		return fmt.Sprintf("undated-%s-%s.md", slug, shortHash)
	}
	stamp := t.UTC().Format("2006-01-02_15-04-05Z")
	return fmt.Sprintf("%s-%s-%s.md", stamp, slug, shortHash)
}

// ShortHash returns the first 12 characters of a sha256 hex string.
func ShortHash(sha256Hex string) string {
	if len(sha256Hex) >= 12 {
		return sha256Hex[:12]
	}
	return sha256Hex
}

// ComputeSHA256 computes the hex sha256 string for the given bytes.
func ComputeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ComputeReaderSHA256 computes sha256 from a reader.
func ComputeReaderSHA256(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
