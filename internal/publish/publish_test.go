package publish

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trustdan/hmba-mail/internal/config"
	"github.com/trustdan/hmba-mail/internal/naming"
)

func setupTestConfig(t *testing.T) (config.Config, string) {
	t.Helper()
	dir := t.TempDir()

	cfg := config.Defaults()
	cfg.Input = filepath.Join(dir, "input")
	cfg.Shared = filepath.Join(dir, "input", "Markdown")
	cfg.Archive = filepath.Join(dir, "archive")
	cfg.State = filepath.Join(dir, "state")
	cfg.Originals = filepath.Join(dir, "originals")

	_ = os.MkdirAll(cfg.Input, 0700)
	_ = os.MkdirAll(cfg.Shared, 0700)
	_ = os.MkdirAll(cfg.Archive, 0700)
	_ = os.MkdirAll(cfg.State, 0700)
	_ = os.MkdirAll(cfg.Originals, 0700)

	return cfg, dir
}

func TestAtomicWriteAndReplace(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "doc.txt")

	if err := AtomicWrite(target, []byte("version 1")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	content, _ := os.ReadFile(target)
	if string(content) != "version 1" {
		t.Fatalf("expected version 1, got %s", string(content))
	}

	if err := AtomicWrite(target, []byte("version 2")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	content, _ = os.ReadFile(target)
	if string(content) != "version 2" {
		t.Fatalf("expected version 2, got %s", string(content))
	}
}

func TestLockPreventsConcurrentRuns(t *testing.T) {
	dir := t.TempDir()
	release1, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	defer release1()

	_, err = AcquireLock(dir)
	if err == nil {
		t.Fatal("expected second lock acquisition to fail")
	}
}

func TestRunDualPublicationAndRepeat(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	// Create 2 test EMLs in input
	eml1 := "From: Canvas <canvas@example.test>\r\n" +
		"Subject: Recent Canvas Notifications\r\n" +
		"Date: Fri, 11 Sep 2026 12:00:00 -0500\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Notification content 1\r\n"

	eml2 := "From: Director <director@example.test>\r\n" +
		"Subject: Fwd: **Weekly Announcement** Sep 11\r\n" +
		"Date: Fri, 11 Sep 2026 13:00:00 -0500\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Announcement content 2\r\n"

	emlUnrelated := "From: Spammer <spam@example.test>\r\n" +
		"Subject: Buy Something Unrelated\r\n" +
		"Date: Fri, 11 Sep 2026 14:00:00 -0500\r\n\r\n" +
		"Spam\r\n"

	_ = os.WriteFile(filepath.Join(cfg.Input, "msg1.eml"), []byte(eml1), 0600)
	_ = os.WriteFile(filepath.Join(cfg.Input, "msg2.eml"), []byte(eml2), 0600)
	_ = os.WriteFile(filepath.Join(cfg.Input, "msg3.eml"), []byte(emlUnrelated), 0600)

	// Also put an EML inside cfg.Shared to test discovery pruning!
	_ = os.WriteFile(filepath.Join(cfg.Shared, "should_be_pruned.eml"), []byte(eml1), 0600)

	// 1. Dry run
	repDry, err := Run(context.Background(), cfg, true)
	if err != nil {
		t.Fatalf("dry run error: %v", err)
	}
	if repDry.Counts.Scanned != 3 || repDry.Counts.Matched != 2 || repDry.Counts.Written != 2 {
		t.Fatalf("unexpected dry run counts: %+v", repDry.Counts)
	}

	// 2. Live run
	repLive, err := Run(context.Background(), cfg, false)
	if err != nil {
		t.Fatalf("live run error: %v", err)
	}
	if repLive.Counts.Scanned != 3 || repLive.Counts.Matched != 2 || repLive.Counts.Written != 2 {
		t.Fatalf("unexpected live run counts: %+v", repLive.Counts)
	}

	// Verify files in Archive
	archCanvas := filepath.Join(cfg.Archive, "canvas-digest")
	entries, _ := os.ReadDir(archCanvas)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in archive/canvas-digest, got %d", len(entries))
	}

	// Verify files in Shared
	sharedCanvas := filepath.Join(cfg.Shared, "canvas-digest")
	entriesShared, _ := os.ReadDir(sharedCanvas)
	if len(entriesShared) != 1 {
		t.Fatalf("expected 1 file in shared/canvas-digest, got %d", len(entriesShared))
	}

	// Verify Originals
	entriesOrig, _ := os.ReadDir(cfg.Originals)
	if len(entriesOrig) != 2 {
		t.Fatalf("expected 2 originals, got %d", len(entriesOrig))
	}

	// 3. Repeat run should be a complete no-op (0 written, 2 unchanged)
	repRepeat, err := Run(context.Background(), cfg, false)
	if err != nil {
		t.Fatalf("repeat run error: %v", err)
	}
	if repRepeat.Counts.Written != 0 || repRepeat.Counts.Unchanged != 2 {
		t.Fatalf("expected 0 written, 2 unchanged; got %+v", repRepeat.Counts)
	}
}

func TestWriteDestinationConflictWhenUserEdited(t *testing.T) {
	cfg, _ := setupTestConfig(t)
	journal, _ := LoadJournal(cfg.State)

	sourceSHA := "source123"
	generatedData := []byte("---\ntitle: \"Test\"\nsource_sha256: \"source123\"\n---\n# Test\nBody\n")
	genSHA := naming.ComputeSHA256(generatedData)

	destPath := filepath.Join(cfg.Archive, "test.md")

	// 1. Initial write
	res, _, err := WriteMarkdownDestination(destPath, generatedData, sourceSHA, journal)
	if err != nil || res != WriteResultCreated {
		t.Fatalf("initial write failed: %v, %v", res, err)
	}
	journal.Record(JournalRecord{
		SourceSHA256:    sourceSHA,
		GeneratedSHA256: genSHA,
	})

	// 2. Unchanged write
	res, _, err = WriteMarkdownDestination(destPath, generatedData, sourceSHA, journal)
	if err != nil || res != WriteResultUnchanged {
		t.Fatalf("expected unchanged, got %v", res)
	}

	// 3. User edits file
	userEdited := []byte("---\ntitle: \"Test\"\nsource_sha256: \"source123\"\n---\n# Test\nUser Modified Body\n")
	_ = os.WriteFile(destPath, userEdited, 0600)

	// 4. Subsequent run should detect conflict and refuse overwrite
	res, reason, err := WriteMarkdownDestination(destPath, generatedData, sourceSHA, journal)
	if res != WriteResultConflict || !strings.Contains(reason, "local edits") {
		t.Fatalf("expected conflict due to user edits, got %v, %s", res, reason)
	}
	// Verify user edits are intact
	content, _ := os.ReadFile(destPath)
	if string(content) != string(userEdited) {
		t.Fatalf("user edit was overwritten!")
	}
}

func TestBackfillExistingArchive(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	// Create 2 archive files
	file1 := filepath.Join(cfg.Archive, "canvas-digest", "2026-07-13-digest.md")
	file2 := filepath.Join(cfg.Archive, "program-announcement", "2026-09-08-announcement.md")
	_ = os.MkdirAll(filepath.Dir(file1), 0700)
	_ = os.MkdirAll(filepath.Dir(file2), 0700)

	_ = os.WriteFile(file1, []byte("# Digest 1"), 0600)
	_ = os.WriteFile(file2, []byte("# Announcement 2"), 0600)

	// 1. Dry run
	repDry, err := Backfill(context.Background(), cfg, true)
	if err != nil {
		t.Fatalf("backfill dry run error: %v", err)
	}
	if repDry.TotalScanned != 2 || repDry.Copied != 2 {
		t.Fatalf("unexpected dry run counts: %+v", repDry)
	}
	// Shared should still be empty
	if _, err := os.Stat(filepath.Join(cfg.Shared, "canvas-digest", "2026-07-13-digest.md")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote files")
	}

	// 2. Live run
	repLive, err := Backfill(context.Background(), cfg, false)
	if err != nil {
		t.Fatalf("backfill live error: %v", err)
	}
	if repLive.TotalScanned != 2 || repLive.Copied != 2 {
		t.Fatalf("unexpected live run counts: %+v", repLive)
	}

	// Verify files in shared
	shared1 := filepath.Join(cfg.Shared, "canvas-digest", "2026-07-13-digest.md")
	data, _ := os.ReadFile(shared1)
	if string(data) != "# Digest 1" {
		t.Fatalf("unexpected shared file data: %s", string(data))
	}

	// 3. Repeat backfill should report unchanged = 2, copied = 0
	repRepeat, err := Backfill(context.Background(), cfg, false)
	if err != nil {
		t.Fatalf("repeat backfill error: %v", err)
	}
	if repRepeat.Copied != 0 || repRepeat.Unchanged != 2 {
		t.Fatalf("repeat backfill expected 0 copied, 2 unchanged; got %+v", repRepeat)
	}
}

func TestRunEmptyInputDirectory(t *testing.T) {
	cfg, _ := setupTestConfig(t)
	// Input directory exists but has 0 EML files
	rep, err := Run(context.Background(), cfg, false)
	if err != nil {
		t.Fatalf("expected clean run on empty directory, got err: %v", err)
	}
	if rep.Counts.Scanned != 0 || rep.Counts.Matched != 0 || rep.Counts.Written != 0 {
		t.Fatalf("expected 0 counts for empty directory, got %+v", rep.Counts)
	}
	if rep.Status != "run_complete" {
		t.Fatalf("expected run_complete status, got %s", rep.Status)
	}
}

func TestRunUnavailableInputDirectoryTimeout(t *testing.T) {
	cfg, _ := setupTestConfig(t)
	cfg.Input = filepath.Join(t.TempDir(), "non-existent-folder")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := Run(ctx, cfg, false)
	if err == nil {
		t.Fatal("expected error on missing input directory, got nil")
	}
}

func TestRunContextCancellation(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	// Create a test EML
	eml1 := "From: Canvas <canvas@example.test>\r\nSubject: Recent Canvas Notifications\r\nDate: Fri, 11 Sep 2026 12:00:00 -0500\r\n\r\nTest\r\n"
	_ = os.WriteFile(filepath.Join(cfg.Input, "msg.eml"), []byte(eml1), 0600)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := Run(ctx, cfg, false)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

