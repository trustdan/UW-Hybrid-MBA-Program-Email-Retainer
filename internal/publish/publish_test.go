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
	for _, message := range repRepeat.Messages {
		if message.ArchiveWritten || message.SharedWritten {
			t.Fatalf("unchanged message reported destination writes: %+v", message)
		}
	}

	// Removing one shared copy should repair only that destination, and the
	// preview and live reports should identify the same work.
	missingShared := filepath.Join(sharedCanvas, entriesShared[0].Name())
	if err := os.Remove(missingShared); err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []bool{true, false} {
		repRepair, err := Run(context.Background(), cfg, dryRun)
		if err != nil {
			t.Fatalf("repair dryRun=%v: %v", dryRun, err)
		}
		if repRepair.Counts.Written != 1 || repRepair.Counts.Unchanged != 1 {
			t.Fatalf("repair dryRun=%v: unexpected counts %+v", dryRun, repRepair.Counts)
		}
		for _, message := range repRepair.Messages {
			wantSharedWrite := message.Category == "canvas-digest"
			if message.ArchiveWritten || message.SharedWritten != wantSharedWrite {
				t.Fatalf("repair dryRun=%v: inaccurate write flags %+v", dryRun, message)
			}
		}
	}
	if _, err := os.Stat(missingShared); err != nil {
		t.Fatalf("shared copy was not restored: %v", err)
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

func TestRunDryRunDoesNotCreateStateDirAndPreviewsConflicts(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.Input = filepath.Join(dir, "input")
	cfg.Archive = filepath.Join(dir, "archive")
	cfg.Shared = filepath.Join(dir, "shared")
	cfg.State = filepath.Join(dir, "state-absent") // Does NOT exist
	cfg.Originals = filepath.Join(dir, "originals-absent")

	_ = os.MkdirAll(cfg.Input, 0700)
	_ = os.MkdirAll(cfg.Archive, 0700)
	_ = os.MkdirAll(cfg.Shared, 0700)

	eml := "From: Canvas <canvas@example.test>\r\n" +
		"Subject: Recent Canvas Notifications\r\n" +
		"Date: Fri, 11 Sep 2026 12:00:00 -0500\r\n\r\n" +
		"Notification\r\n"
	_ = os.WriteFile(filepath.Join(cfg.Input, "test.eml"), []byte(eml), 0600)

	// Pre-create an edited destination file in Archive to test conflict preview in dry-run
	catDir := filepath.Join(cfg.Archive, "canvas-digest")
	_ = os.MkdirAll(catDir, 0700)
	when, found := naming.ParseEmailDate("Fri, 11 Sep 2026 12:00:00 -0500")
	destName := naming.FormatDateFilename(when, found, "Recent Canvas Notifications", naming.ComputeSHA256([]byte(eml)))
	_ = os.WriteFile(filepath.Join(catDir, destName), []byte("user manual edit without source sha"), 0600)

	repDry, err := Run(context.Background(), cfg, true)
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}

	// 1. Critical requirement: absent state directory must NOT be created by dry-run
	if _, err := os.Stat(cfg.State); !os.IsNotExist(err) {
		t.Fatalf("dry-run created state directory on disk: %s", cfg.State)
	}
	if _, err := os.Stat(cfg.Originals); !os.IsNotExist(err) {
		t.Fatalf("dry-run created originals directory on disk: %s", cfg.Originals)
	}

	// 2. Verified matched message
	if repDry.Counts.Matched != 1 {
		t.Fatalf("expected 1 matched message, got %d", repDry.Counts.Matched)
	}
	if repDry.Counts.Conflicts != 1 || repDry.Counts.Written != 0 {
		t.Fatalf("expected conflict preview, got %+v", repDry.Counts)
	}
}

func TestAtomicWritePreservesDirectory(t *testing.T) {
	target := filepath.Join(t.TempDir(), "existing-directory")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(target, []byte("replacement")); err == nil {
		t.Fatal("expected replacement of directory to fail")
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("original directory was lost: %v", err)
	}
}

func TestEmptyRunReportsPersistenceFailure(t *testing.T) {
	cfg, _ := setupTestConfig(t)
	if err := os.Mkdir(filepath.Join(cfg.State, "last-run.json"), 0700); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(context.Background(), cfg, false)
	if err == nil || rep == nil || len(rep.Errors) == 0 || rep.Status != "run_completed_with_issues" {
		t.Fatalf("expected report persistence failure: report=%+v err=%v", rep, err)
	}
}

func TestSharedConflictPreventsArchiveMutation(t *testing.T) {
	cfg, _ := setupTestConfig(t)
	raw := []byte("From: canvas@example.test\r\nSubject: Recent Canvas Notifications\r\n\r\nSynthetic message\r\n")
	if err := os.WriteFile(filepath.Join(cfg.Input, "message.eml"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := Run(context.Background(), cfg, true)
	if err != nil || len(preview.Messages) != 1 {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	msg := preview.Messages[0]
	shared := filepath.Join(cfg.Shared, msg.Category, msg.Filename)
	if err := os.MkdirAll(filepath.Dir(shared), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, []byte("manual edits"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []bool{true, false} {
		rep, err := Run(context.Background(), cfg, dryRun)
		if err != nil || rep.Counts.Conflicts != 1 || rep.Messages[0].ArchiveWritten {
			t.Fatalf("dryRun=%v: report=%+v err=%v", dryRun, rep, err)
		}
		if _, err := os.Stat(filepath.Join(cfg.Archive, msg.Category, msg.Filename)); !os.IsNotExist(err) {
			t.Fatal("archive was created despite known shared conflict")
		}
	}
}

func TestBackfillLockPreventsConcurrentRunWithLive(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	// Acquire live lock on cfg.State
	releaseLock, err := AcquireLock(cfg.State)
	if err != nil {
		t.Fatalf("acquire initial lock: %v", err)
	}
	defer releaseLock()

	// Live backfill must fail because lock is held
	_, err = Backfill(context.Background(), cfg, false)
	if err == nil {
		t.Fatal("expected backfill to fail while live lock is held, got nil")
	}
	if !strings.Contains(err.Error(), "run lock active") {
		t.Fatalf("expected 'run lock active' error, got: %v", err)
	}
}

func TestRunEmptyInputSavesLastRun(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	rep, err := Run(context.Background(), cfg, false)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if rep.Counts.Scanned != 0 {
		t.Fatalf("expected 0 scanned, got %d", rep.Counts.Scanned)
	}

	lastRunPath := filepath.Join(cfg.State, "last-run.json")
	data, err := os.ReadFile(lastRunPath)
	if err != nil {
		t.Fatalf("expected last-run.json to be saved for empty run: %v", err)
	}
	if !strings.Contains(string(data), `"scanned": 0`) {
		t.Fatalf("unexpected last-run content: %s", string(data))
	}
}

func TestRunCorruptJournalReturnsError(t *testing.T) {
	cfg, _ := setupTestConfig(t)
	jPath := filepath.Join(cfg.State, "journal.json")
	if err := os.WriteFile(jPath, []byte("NOT_VALID_JSON{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), cfg, false)
	if err == nil {
		t.Fatal("expected error on corrupt journal in Run, got nil")
	}
	if !strings.Contains(err.Error(), "corrupt journal") {
		t.Fatalf("expected 'corrupt journal' error, got: %v", err)
	}
}
