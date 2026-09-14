package gitpub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPreflight_WindowsShortPath(t *testing.T) {
	localRepo, _ := initTestRepo(t)
	longPath, err := syscall.UTF16PtrFromString(localRepo)
	if err != nil {
		t.Fatal(err)
	}
	size, err := syscall.GetShortPathName(longPath, nil, 0)
	if err != nil {
		t.Fatalf("get short path size: %v", err)
	}
	buf := make([]uint16, size)
	n, err := syscall.GetShortPathName(longPath, &buf[0], size)
	if err != nil || n >= size {
		t.Fatalf("get short path: length=%d, size=%d, err=%v", n, size, err)
	}
	shortPath := syscall.UTF16ToString(buf[:n])
	if strings.EqualFold(shortPath, localRepo) {
		t.Skip("8.3 short names are not available on this filesystem")
	}
	actualRoot, err := FindRepoRoot(context.Background(), shortPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(shortPath, actualRoot) {
		t.Skip("Git preserved the short path; no short/long path mismatch to test")
	}
	if err := Preflight(context.Background(), shortPath, "origin", "main", nil, nil); err != nil {
		t.Fatalf("expected preflight to accept short path %q (Git reports %q), got: %v", shortPath, actualRoot, err)
	}
	ctx := context.Background()
	archivePath := filepath.Join(localRepo, "emails", "archive", "canvas-digest", "message.md")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, []byte("# Synthetic message"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, shortPath, "add", "--", "emails/archive"); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(ctx, shortPath, "origin", "main", []string{archivePath}, nil); err != nil {
		t.Fatalf("preflight with mixed short/long paths: %v", err)
	}
	if sha, err := StageAndCommit(ctx, shortPath, []string{archivePath}, nil); err != nil || sha == "" {
		t.Fatalf("commit with mixed short/long paths: sha=%q, err=%v", sha, err)
	}
}
