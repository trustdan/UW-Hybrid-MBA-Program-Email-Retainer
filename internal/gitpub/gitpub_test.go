package gitpub

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo initializes a temporary Git repository with git user/email configured.
func initTestRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()

	// 1. Create bare remote
	bareRemote := filepath.Join(dir, "remote.git")
	cmd := exec.Command("git", "init", "--bare", "-b", "main", bareRemote)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init bare remote: %v (%s)", err, out)
	}

	// 2. Create local repo
	localRepo := filepath.Join(dir, "local")
	cmd = exec.Command("git", "init", "-b", "main", localRepo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init local repo: %v (%s)", err, out)
	}

	// Configure git user
	_ = exec.Command("git", "-C", localRepo, "config", "user.name", "Test User").Run()
	_ = exec.Command("git", "-C", localRepo, "config", "user.email", "test@example.com").Run()
	_ = exec.Command("git", "-C", localRepo, "config", "commit.gpgsign", "false").Run()

	// Set remote
	_ = exec.Command("git", "-C", localRepo, "remote", "add", "origin", bareRemote).Run()

	// Initial root commit
	readme := filepath.Join(localRepo, "README.md")
	_ = os.WriteFile(readme, []byte("# Parent Repo"), 0600)
	_ = exec.Command("git", "-C", localRepo, "add", "README.md").Run()
	_ = exec.Command("git", "-C", localRepo, "commit", "-m", "Initial commit").Run()
	_ = exec.Command("git", "-C", localRepo, "push", "-u", "origin", "main").Run()

	return localRepo, bareRemote
}

func TestPreflight_CleanState(t *testing.T) {
	localRepo, _ := initTestRepo(t)
	journal, _ := LoadGitJournal(t.TempDir())

	ctx := context.Background()
	err := Preflight(ctx, localRepo, "origin", "main", nil, journal)
	if err != nil {
		t.Fatalf("expected preflight to pass on clean repo, got: %v", err)
	}
}

func TestPreflight_RejectsUnrelatedStagedFiles(t *testing.T) {
	localRepo, _ := initTestRepo(t)
	journal, _ := LoadGitJournal(t.TempDir())

	// Stage an unrelated file (e.g. personal note)
	personalFile := filepath.Join(localRepo, "notes.txt")
	_ = os.WriteFile(personalFile, []byte("private notes"), 0600)
	_ = exec.Command("git", "-C", localRepo, "add", "notes.txt").Run()

	ctx := context.Background()
	err := Preflight(ctx, localRepo, "origin", "main", nil, journal)
	if err == nil {
		t.Fatal("expected preflight to fail when unrelated staged files exist, but passed")
	}
}

func TestPreflight_RejectsStaleSubmodule(t *testing.T) {
	localRepo, _ := initTestRepo(t)
	journal, _ := LoadGitJournal(t.TempDir())

	// Simulate stale emails/.git
	staleGit := filepath.Join(localRepo, "emails", ".git")
	_ = os.MkdirAll(staleGit, 0700)

	ctx := context.Background()
	err := Preflight(ctx, localRepo, "origin", "main", nil, journal)
	if err == nil {
		t.Fatal("expected preflight to fail when emails/.git exists, but passed")
	}
}

func TestPreflight_RejectsDetachedHEAD(t *testing.T) {
	localRepo, _ := initTestRepo(t)
	journal, _ := LoadGitJournal(t.TempDir())

	// Detach HEAD
	_ = exec.Command("git", "-C", localRepo, "checkout", "--detach").Run()

	ctx := context.Background()
	err := Preflight(ctx, localRepo, "origin", "main", nil, journal)
	if err == nil {
		t.Fatal("expected preflight to fail on detached HEAD, but passed")
	}
}

func TestStageAndCommit_OnlyArchiveFiles(t *testing.T) {
	localRepo, _ := initTestRepo(t)
	journal, _ := LoadGitJournal(t.TempDir())

	// 1. Attempt to stage a file outside emails/archive/
	badPath := filepath.Join(localRepo, "emails", "tools", "script.py")
	_ = os.MkdirAll(filepath.Dir(badPath), 0700)
	_ = os.WriteFile(badPath, []byte("print(1)"), 0600)

	ctx := context.Background()
	_, err := StageAndCommit(ctx, localRepo, []string{badPath}, journal)
	if err == nil {
		t.Fatal("expected StageAndCommit to reject file outside emails/archive/, but passed")
	}

	// 2. Stage valid archive files
	arch1 := filepath.Join(localRepo, "emails", "archive", "canvas-digest", "2026-09-12-test.md")
	_ = os.MkdirAll(filepath.Dir(arch1), 0700)
	_ = os.WriteFile(arch1, []byte("# Digest"), 0600)

	commitSHA, err := StageAndCommit(ctx, localRepo, []string{arch1}, journal)
	if err != nil {
		t.Fatalf("expected StageAndCommit to succeed, got: %v", err)
	}
	if commitSHA == "" {
		t.Fatal("expected non-empty commit SHA")
	}

	// Verify journal recorded the commit
	if !journal.IsToolCommit(commitSHA) {
		t.Errorf("expected commit %s to be recorded in journal", commitSHA)
	}

	// 3. Repeat with no changes returns empty string
	repeatSHA, err := StageAndCommit(ctx, localRepo, []string{arch1}, journal)
	if err != nil || repeatSHA != "" {
		t.Fatalf("expected repeat StageAndCommit to return empty string without error, got sha=%s, err=%v", repeatSHA, err)
	}
}

func TestPush_And_InterruptedRecovery(t *testing.T) {
	localRepo, _ := initTestRepo(t)
	journal, _ := LoadGitJournal(t.TempDir())
	ctx := context.Background()

	// 1. Create an archive file and commit it
	arch := filepath.Join(localRepo, "emails", "archive", "canvas-digest", "2026-09-12-msg.md")
	_ = os.MkdirAll(filepath.Dir(arch), 0700)
	_ = os.WriteFile(arch, []byte("# Email content"), 0600)

	sha, err := StageAndCommit(ctx, localRepo, []string{arch}, journal)
	if err != nil {
		t.Fatalf("StageAndCommit failed: %v", err)
	}

	// Verify pending push recorded
	if journal.PendingPushSHA != sha {
		t.Fatalf("expected PendingPushSHA=%s, got %s", sha, journal.PendingPushSHA)
	}

	// 2. Test RecoverPending
	recovered, err := RecoverPending(ctx, localRepo, "origin", "main", journal)
	if err != nil || !recovered {
		t.Fatalf("RecoverPending failed: recovered=%v, err=%v", recovered, err)
	}

	// Verify PendingPushSHA cleared
	if journal.PendingPushSHA != "" {
		t.Errorf("expected PendingPushSHA to be empty after recovery, got %s", journal.PendingPushSHA)
	}
	if journal.LastPushSHA != sha {
		t.Errorf("expected LastPushSHA=%s, got %s", sha, journal.LastPushSHA)
	}

	// 3. Unchanged run has no pending push
	recoveredAgain, err := RecoverPending(ctx, localRepo, "origin", "main", journal)
	if err != nil || recoveredAgain {
		t.Fatalf("expected RecoverPending false, got %v, err=%v", recoveredAgain, err)
	}
}
