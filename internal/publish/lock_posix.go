//go:build !windows

package publish

import (
	"os"
	"syscall"
)

// acquireOSLock attempts to acquire an OS-level exclusive non-blocking lock via flock on POSIX systems.
func acquireOSLock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// releaseOSLock unlocks the file on POSIX systems.
func releaseOSLock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
