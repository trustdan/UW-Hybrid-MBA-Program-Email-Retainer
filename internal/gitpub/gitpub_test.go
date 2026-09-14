package gitpub

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	localRepo := filepath.Join(dir, "local repo with spaces")
	cmd = exec.Command("git", "init", "-b", "main", localRepo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init local repo: %v (%s)", err, out)
	}

	// Initial root commit
	readme := filepath.Join(localRepo, "README.md")
	if err := os.WriteFile(readme, []byte("# Parent Repo"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"config", "user.name", "Test User"},
		{"config", "user.email", "test@example.com"},
		{"config", "commit.gpgsign", "false"},
		{"remote", "add", "origin", bareRemote},
		{"add", "README.md"},
		{"commit", "-m", "Initial commit"},
		{"push", "-u", "origin", "main"},
	} {
		if _, err := runGit(context.Background(), localRepo, args...); err != nil {
			t.Fatalf("initialize test repository: %v", err)
		}
	}

	return localRepo, bareRemote
}

func TestPreflight_WorktreeOperationInProgress(t *testing.T) {
	repo, _ := initTestRepo(t)
	worktree := filepath.Join(t.TempDir(), "linked worktree")
	ctx := context.Background()
	if _, err := runGit(ctx, repo, "worktree", "add", "-b", "review", worktree); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(ctx, worktree, "origin", "review", nil, nil); err != nil {
		t.Fatal(err)
	}
	gitDir, err := runGit(ctx, worktree, "rev-parse", "--git-dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.FromSlash(gitDir), "MERGE_HEAD"), []byte("pending"), 0600); err != nil {
		t.Fatal(err)
	}
	err = Preflight(ctx, worktree, "origin", "review", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "git operation in progress") {
		t.Fatalf("expected worktree merge to block publishing, got: %v", err)
	}
}

func TestArchivePaths_PreserveSpecialCharacters(t *testing.T) {
	repo, _ := initTestRepo(t)
	ctx := context.Background()
	names := []string{"résumé [draft].md", " leading space.md", "[ab].md", "a.md"}
	if runtime.GOOS != "windows" {
		names = append(names, "line\nbreak.md", "tab\tname.md", "trailing.md ")
	}
	var paths []string
	for _, name := range names {
		path := filepath.Join(repo, "emails", "archive", "canvas-digest", name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# Synthetic archive"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	journal, err := LoadGitJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// [ab].md must not also stage a.md through Git's wildcard matching.
	sha, err := StageAndCommit(ctx, repo, paths[2:3], journal)
	if err != nil || sha == "" {
		t.Fatalf("literal filename commit: sha=%q, err=%v", sha, err)
	}
	if _, err := runGit(ctx, repo, "add", "--", "emails/archive"); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(ctx, repo, "origin", "main", paths, journal); err != nil {
		t.Fatalf("preflight with special filenames: %v", err)
	}
	if _, err := StageAndCommit(ctx, repo, paths, journal); err != nil {
		t.Fatal(err)
	}
	if recovered, err := RecoverPending(ctx, repo, "origin", "main", journal); err != nil || !recovered {
		t.Fatalf("recover special filenames: recovered=%v, err=%v", recovered, err)
	}
	// Alias normalization must continue to allow staging deletions.
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	if sha, err := StageAndCommit(ctx, repo, paths[:1], journal); err != nil || sha == "" {
		t.Fatalf("commit deletion: sha=%q, err=%v", sha, err)
	}
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

func TestPreflight_RejectsSubdirectoryAsRoot(t *testing.T) {
	localRepo, _ := initTestRepo(t)
	subdir := filepath.Join(localRepo, "subdirectory")
	if err := os.Mkdir(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	err := Preflight(context.Background(), subdir, "origin", "main", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "does not match discovered root") {
		t.Fatalf("expected root mismatch for a subdirectory, got: %v", err)
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

func TestRecoveryRefusesUnsafeRepositoryWithoutChangingRemote(t *testing.T) {
	for _, scenario := range []string{"personal commit", "wrong branch", "missing upstream"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			repo, remote := initTestRepo(t)
			before, err := runGit(ctx, remote, "rev-parse", "main")
			if err != nil {
				t.Fatal(err)
			}
			journal, err := LoadGitJournal(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(repo, "emails", "archive", "note.md")
			if err := os.MkdirAll(filepath.Dir(archive), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(archive, []byte("email"), 0600); err != nil {
				t.Fatal(err)
			}
			pending, err := StageAndCommit(ctx, repo, []string{archive}, journal)
			if err != nil {
				t.Fatal(err)
			}
			var commands [][]string
			switch scenario {
			case "personal commit":
				if err := os.WriteFile(filepath.Join(repo, "personal.txt"), []byte("private work"), 0600); err != nil {
					t.Fatal(err)
				}
				commands = [][]string{{"add", "personal.txt"}, {"commit", "-m", "personal work"}}
			case "wrong branch":
				commands = [][]string{{"checkout", "-b", "personal"}}
			case "missing upstream":
				commands = [][]string{{"update-ref", "-d", "refs/remotes/origin/main"}}
			}
			for _, args := range commands {
				if _, err := runGit(ctx, repo, args...); err != nil {
					t.Fatal(err)
				}
			}
			if recovered, err := RecoverPending(ctx, repo, "origin", "main", journal); err == nil || recovered {
				t.Fatalf("unsafe recovery succeeded: recovered=%v, err=%v", recovered, err)
			}
			after, err := runGit(ctx, remote, "rev-parse", "main")
			if err != nil {
				t.Fatal(err)
			}
			if after != before || journal.PendingPushSHA != pending {
				t.Fatal("refused recovery changed remote or cleared pending state")
			}
		})
	}
}

func TestGitJournalRejectsCorruptOrUnreadableState(t *testing.T) {
	for _, content := range []string{"null", "{broken", "[]"} {
		t.Run(content, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "git-journal.json"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadGitJournal(dir); err == nil {
				t.Fatal("invalid journal accepted")
			}
		})
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "git-journal.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGitJournal(dir); err == nil {
		t.Fatal("unreadable journal accepted")
	}
}

func TestToolCommitIdentityRequiresExactNonemptySHA(t *testing.T) {
	journal := &GitJournal{ToolCommits: []string{"", "abc123"}}
	if journal.IsToolCommit("") || journal.IsToolCommit("abc") || journal.IsToolCommit("abc123456") {
		t.Fatal("empty or prefix SHA incorrectly trusted as a recorded commit")
	}
	if !journal.IsToolCommit("abc123") {
		t.Fatal("recorded commit not recognized")
	}
}

func TestGitJournalPersistenceFailuresAreReported(t *testing.T) {
	ctx := context.Background()
	repo, _ := initTestRepo(t)
	journal, err := LoadGitJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the destination forces replacement to fail. Save must not
	// remove that existing destination or silently discard the error.
	if err := os.Mkdir(journal.path, 0700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(repo, "emails", "archive", "note.md")
	if err := os.MkdirAll(filepath.Dir(archive), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte("email"), 0600); err != nil {
		t.Fatal(err)
	}
	sha, err := StageAndCommit(ctx, repo, []string{archive}, journal)
	if err == nil || sha == "" {
		t.Fatalf("commit persistence failure hidden: sha=%q, err=%v", sha, err)
	}
	if info, err := os.Stat(journal.path); err != nil || !info.IsDir() {
		t.Fatal("save destroyed existing destination")
	}
	if err := Push(ctx, repo, "origin", "main", sha, journal); err == nil || !strings.Contains(err.Error(), "persist successful push") {
		t.Fatalf("push persistence failure hidden: %v", err)
	}
}
