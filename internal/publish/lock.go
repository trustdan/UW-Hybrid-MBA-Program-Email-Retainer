package publish

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// AcquireLock acquires an OS-backed single-run lock file in the state directory.
// Returns a release function, or an error if another run is active.
// If a previous process holding the lock crashed or was terminated, the OS automatically
// released the kernel lock, so this will acquire the lock without manual intervention
// while safely preventing concurrent runs from racing.
func AcquireLock(stateDir string) (func(), error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	lockPath := filepath.Join(stateDir, "import.lock")

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockPath, err)
	}

	if err := acquireOSLock(f); err != nil {
		// Another active process holds the OS lock
		pidData, _ := io.ReadAll(f)
		_ = f.Close()
		pidStr := strings.TrimSpace(string(pidData))
		if pidStr == "" {
			pidStr = "unknown"
		}
		return nil, fmt.Errorf("run lock active (PID: %s); another importer is running", pidStr)
	}

	// Acquired OS lock! Update PID for diagnostics
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_, _ = f.WriteString(strconv.Itoa(os.Getpid()))

	release := func() {
		_ = releaseOSLock(f)
		_ = f.Close()
		_ = os.Remove(lockPath)
	}
	return release, nil
}

// CheckLock inspects whether the lock is currently held by an active process.
// Returns (isLocked, pid, error).
func CheckLock(stateDir string) (bool, string, error) {
	lockPath := filepath.Join(stateDir, "import.lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		return false, "", nil
	}

	f, err := os.OpenFile(lockPath, os.O_RDWR, 0600)
	if err != nil {
		// File exists but can't be opened - likely held exclusively on Windows
		pidData, _ := os.ReadFile(lockPath)
		return true, strings.TrimSpace(string(pidData)), nil
	}
	defer f.Close()

	if err := acquireOSLock(f); err != nil {
		// Locked by another active process
		pidData, _ := io.ReadAll(f)
		return true, strings.TrimSpace(string(pidData)), nil
	}

	// Lock was acquirable, meaning no active process is holding it
	_ = releaseOSLock(f)
	return false, "", nil
}
