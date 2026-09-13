package publish

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireLock_Lifecycle(t *testing.T) {
	tempDir := t.TempDir()

	// 1. First acquire succeeds
	rel1, err := AcquireLock(tempDir)
	if err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}

	// Verify CheckLock reports locked
	locked, _, err := CheckLock(tempDir)
	if err != nil || !locked {
		t.Errorf("expected CheckLock true, got locked=%v, err=%v", locked, err)
	}

	// 2. Second acquire concurrently fails
	_, err = AcquireLock(tempDir)
	if err == nil {
		rel1()
		t.Fatal("expected second AcquireLock to fail while first is held, but succeeded")
	}

	// 3. Release first
	rel1()

	// Verify CheckLock reports not locked
	locked, _, _ = CheckLock(tempDir)
	if locked {
		t.Errorf("expected CheckLock false after release, got true")
	}

	// 4. Now acquire succeeds again
	rel2, err := AcquireLock(tempDir)
	if err != nil {
		t.Fatalf("AcquireLock after release failed: %v", err)
	}
	rel2()
}

func TestAcquireLock_UncleanExitRecovery(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "import.lock")

	// Simulate a dead process leaving a lock file with PID 999999
	if err := os.WriteFile(lockPath, []byte("999999"), 0600); err != nil {
		t.Fatalf("write stale lock file: %v", err)
	}

	// Because no process holds an OS handle/lock on this file, AcquireLock should succeed
	rel, err := AcquireLock(tempDir)
	if err != nil {
		t.Fatalf("expected AcquireLock to succeed over dead process lock file, got: %v", err)
	}
	rel()

	// File should be removed upon release
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("expected lock file to be removed after release")
	}
}
