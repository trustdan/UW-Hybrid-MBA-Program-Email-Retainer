// Package gitpub implements safe, non-interactive Git preflight, scoped archive commits,
// and fast-forward pushes to the parent repository.
package gitpub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GitJournal tracks tool-created commits and push status for interruption recovery.
type GitJournal struct {
	mu             sync.Mutex
	path           string
	LastCommitSHA  string    `json:"last_commit_sha,omitempty"`
	PendingPushSHA string    `json:"pending_push_sha,omitempty"`
	LastPushSHA    string    `json:"last_push_sha,omitempty"`
	LastPushAt     time.Time `json:"last_push_at,omitempty"`
	ToolCommits    []string  `json:"tool_commits,omitempty"`
}

// LoadGitJournal loads or initializes the Git journal from stateDir.
func LoadGitJournal(stateDir string) (*GitJournal, error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	path := filepath.Join(stateDir, "git-journal.json")
	gj := &GitJournal{
		path:        path,
		ToolCommits: []string{},
	}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, gj)
	}
	return gj, nil
}

// Save writes the Git journal atomically.
func (gj *GitJournal) Save() error {
	gj.mu.Lock()
	defer gj.mu.Unlock()
	data, err := json.MarshalIndent(gj, "", "  ")
	if err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(gj.path), ".git-journal-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, gj.path); err == nil {
		return nil
	}
	// Windows fallback
	_ = os.Remove(gj.path)
	return os.Rename(tmpPath, gj.path)
}

// RecordToolCommit registers a commit created by this tool.
func (gj *GitJournal) RecordToolCommit(sha string) {
	gj.mu.Lock()
	defer gj.mu.Unlock()
	gj.LastCommitSHA = sha
	gj.PendingPushSHA = sha
	for _, s := range gj.ToolCommits {
		if s == sha {
			return
		}
	}
	gj.ToolCommits = append(gj.ToolCommits, sha)
}

// IsToolCommit checks if a commit was recorded as created by this tool.
func (gj *GitJournal) IsToolCommit(sha string) bool {
	gj.mu.Lock()
	defer gj.mu.Unlock()
	for _, s := range gj.ToolCommits {
		if strings.HasPrefix(s, sha) || strings.HasPrefix(sha, s) {
			return true
		}
	}
	return false
}

// ClearPendingPush updates push status after successful push.
func (gj *GitJournal) ClearPendingPush(pushedSHA string) {
	gj.mu.Lock()
	defer gj.mu.Unlock()
	gj.LastPushSHA = pushedSHA
	gj.PendingPushSHA = ""
	gj.LastPushAt = time.Now().UTC()
}

// runGit executes a Git command with non-interactive flags and argument arrays.
// It never invokes a shell and disables interactive terminal prompts.
func runGit(ctx context.Context, repoDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir

	// Ensure no interactive credential or gpg prompts hang the process
	env := os.Environ()
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=")
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("git %s failed: %w (output: %s)", strings.Join(args, " "), err, errMsg)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// FindRepoRoot discovers the root directory of the parent Git repository.
func FindRepoRoot(ctx context.Context, startDir string) (string, error) {
	out, err := runGit(ctx, startDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("find git repository root from %s: %w", startDir, err)
	}
	return filepath.Clean(filepath.FromSlash(out)), nil
}

// Preflight verifies the repository is in a safe state for automated commits.
func Preflight(ctx context.Context, repoRoot string, expectedRemote, expectedBranch string, allowedStagedFiles []string, journal *GitJournal) error {
	// 1. Verify repo root matches
	actualRoot, err := FindRepoRoot(ctx, repoRoot)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actualRoot, filepath.Clean(repoRoot)) {
		return fmt.Errorf("configured repo root %s does not match discovered root %s", repoRoot, actualRoot)
	}

	// 2. Reject stale nested submodule repository (emails/.git must not exist)
	nestedGit := filepath.Join(repoRoot, "emails", ".git")
	if _, err := os.Stat(nestedGit); !os.IsNotExist(err) {
		return fmt.Errorf("stale nested git repository detected at %s; please remove before publishing", nestedGit)
	}

	// 3. Verify current branch is expectedBranch and not detached HEAD
	currentRef, err := runGit(ctx, repoRoot, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return fmt.Errorf("git is in detached HEAD state; automated publishing requires branch %s", expectedBranch)
	}
	expectedRef := "refs/heads/" + expectedBranch
	if currentRef != expectedRef {
		return fmt.Errorf("current branch is %s, expected %s", currentRef, expectedRef)
	}

	// 4. Verify no merge, rebase, or cherry-pick in progress
	gitDirOut, err := runGit(ctx, repoRoot, "rev-parse", "--git-dir")
	if err != nil {
		return fmt.Errorf("locate .git dir: %w", err)
	}
	gitDir := filepath.Join(repoRoot, filepath.FromSlash(gitDirOut))
	if !filepath.IsAbs(gitDirOut) {
		gitDir = filepath.Clean(filepath.Join(repoRoot, gitDirOut))
	}

	conflicts := []string{"MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD"}
	for _, marker := range conflicts {
		if _, err := os.Stat(filepath.Join(gitDir, marker)); !os.IsNotExist(err) {
			return fmt.Errorf("git operation in progress (%s); publishing aborted", marker)
		}
	}

	// 5. Inspect pre-existing staged files
	stagedOut, err := runGit(ctx, repoRoot, "diff", "--cached", "--name-only")
	if err != nil {
		return fmt.Errorf("check staged files: %w", err)
	}

	if stagedOut != "" {
		allowedMap := make(map[string]bool)
		for _, f := range allowedStagedFiles {
			rel, _ := filepath.Rel(repoRoot, f)
			allowedMap[filepath.ToSlash(rel)] = true
		}

		stagedLines := strings.Split(stagedOut, "\n")
		var unrelated []string
		for _, line := range stagedLines {
			line = strings.TrimSpace(line)
			if line != "" && !allowedMap[filepath.ToSlash(line)] {
				unrelated = append(unrelated, line)
			}
		}

		if len(unrelated) > 0 {
			return fmt.Errorf("unrelated staged files present (%d files, e.g. %s); refusing automated commit", len(unrelated), unrelated[0])
		}
	}

	// 6. Inspect unpushed outgoing commits (refuse unexplained commits that would leak personal work)
	upstreamRef := fmt.Sprintf("%s/%s", expectedRemote, expectedBranch)
	if _, err := runGit(ctx, repoRoot, "rev-parse", "--verify", upstreamRef); err == nil {
		unpushedOut, err := runGit(ctx, repoRoot, "rev-list", fmt.Sprintf("%s..HEAD", upstreamRef))
		if err == nil && strings.TrimSpace(unpushedOut) != "" {
			unpushedSHAs := strings.Fields(unpushedOut)
			for _, sha := range unpushedSHAs {
				if journal == nil || !journal.IsToolCommit(sha) {
					return fmt.Errorf("unpushed personal/unrecorded commit detected (%s); refusing to push", sha[:8])
				}
			}
		}
	}

	return nil
}

// StageAndCommit stages ONLY the exact specified archive files and creates an automated commit.
// Returns the new commit SHA (or empty string if nothing needed to be committed).
func StageAndCommit(ctx context.Context, repoRoot string, archiveFiles []string, journal *GitJournal) (string, error) {
	if len(archiveFiles) == 0 {
		return "", nil
	}

	// 1. Verify every file path is inside emails/archive/
	var relPaths []string
	for _, path := range archiveFiles {
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return "", fmt.Errorf("rel path for %s: %w", path, err)
		}
		normRel := filepath.ToSlash(rel)
		if !strings.HasPrefix(normRel, "emails/archive/") {
			return "", fmt.Errorf("security violation: refusing to stage file outside emails/archive/: %s", normRel)
		}
		relPaths = append(relPaths, normRel)
	}

	// 2. Stage only the exact managed archive files
	for _, rel := range relPaths {
		if _, err := runGit(ctx, repoRoot, "add", "--", rel); err != nil {
			return "", fmt.Errorf("git add %s: %w", rel, err)
		}
	}

	// 3. Verify staged diff contains ONLY the intended files
	stagedOut, err := runGit(ctx, repoRoot, "diff", "--cached", "--name-only")
	if err != nil {
		return "", fmt.Errorf("verify staged files: %w", err)
	}
	if stagedOut == "" {
		// Nothing actually changed
		return "", nil
	}

	intendedMap := make(map[string]bool)
	for _, r := range relPaths {
		intendedMap[r] = true
	}

	for _, line := range strings.Split(stagedOut, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !intendedMap[filepath.ToSlash(line)] {
			// Unstage everything before returning
			_, _ = runGit(ctx, repoRoot, "reset", "HEAD")
			return "", fmt.Errorf("security violation: unexpected file staged (%s); un-staged all", line)
		}
	}

	// 4. Create commit
	msg := fmt.Sprintf("Add/update %d email archive file(s) [automated]", len(relPaths))
	if _, err := runGit(ctx, repoRoot, "commit", "-m", msg, "--no-edit"); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	// 5. Get commit SHA
	commitSHA, err := runGit(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w", err)
	}

	if journal != nil {
		journal.RecordToolCommit(commitSHA)
		_ = journal.Save()
	}

	return commitSHA, nil
}

// Push performs a fast-forward push of the branch to the remote and verifies reachability.
func Push(ctx context.Context, repoRoot string, remote, branch string, commitSHA string, journal *GitJournal) error {
	// 1. Fetch remote branch
	if _, err := runGit(ctx, repoRoot, "fetch", remote, branch); err != nil {
		return fmt.Errorf("git fetch %s %s: %w", remote, branch, err)
	}

	// 2. Check for remote divergence
	remoteRef := fmt.Sprintf("%s/%s", remote, branch)
	divergence, err := runGit(ctx, repoRoot, "rev-list", fmt.Sprintf("HEAD..%s", remoteRef), "--count")
	if err == nil && strings.TrimSpace(divergence) != "0" {
		return fmt.Errorf("remote %s is ahead by %s commit(s); safe fast-forward not possible, manual reconciliation required", remoteRef, divergence)
	}

	// 3. Push to remote
	if _, err := runGit(ctx, repoRoot, "push", remote, branch); err != nil {
		return fmt.Errorf("git push %s %s: %w", remote, branch, err)
	}

	// 4. Verify commit is reachable on remote
	if commitSHA != "" {
		if _, err := runGit(ctx, repoRoot, "merge-base", "--is-ancestor", commitSHA, remoteRef); err != nil {
			return fmt.Errorf("commit %s not reachable on %s after push: %w", commitSHA[:8], remoteRef, err)
		}
	}

	if journal != nil {
		journal.ClearPendingPush(commitSHA)
		_ = journal.Save()
	}

	return nil
}

// RecoverPending checks if an earlier commit is pending push and pushes it safely if verified.
func RecoverPending(ctx context.Context, repoRoot string, remote, branch string, journal *GitJournal) (bool, error) {
	if journal == nil || journal.PendingPushSHA == "" {
		return false, nil
	}

	sha := journal.PendingPushSHA

	// 1. Verify commit exists
	if _, err := runGit(ctx, repoRoot, "cat-file", "-e", sha); err != nil {
		return false, fmt.Errorf("pending commit %s does not exist in repository", sha)
	}

	// 2. Verify commit touched ONLY emails/archive/ files
	diffOut, err := runGit(ctx, repoRoot, "diff-tree", "--no-commit-id", "--name-only", "-r", sha)
	if err != nil {
		return false, fmt.Errorf("diff-tree for %s: %w", sha, err)
	}
	for _, line := range strings.Split(diffOut, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(filepath.ToSlash(line), "emails/archive/") {
			return false, fmt.Errorf("pending commit %s touched files outside emails/archive/: %s; refusing recovery push", sha[:8], line)
		}
	}

	// 3. Verify commit is an ancestor of HEAD
	if _, err := runGit(ctx, repoRoot, "merge-base", "--is-ancestor", sha, "HEAD"); err != nil {
		return false, fmt.Errorf("pending commit %s is not an ancestor of current HEAD", sha[:8])
	}

	// 4. Push
	if err := Push(ctx, repoRoot, remote, branch, sha, journal); err != nil {
		return false, fmt.Errorf("recover pending push %s: %w", sha[:8], err)
	}

	return true, nil
}
