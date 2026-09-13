//go:build windows

package publish

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

// acquireOSLock attempts to acquire an OS-level exclusive non-blocking lock on the file on Windows.
func acquireOSLock(f *os.File) error {
	var ol syscall.Overlapped
	r1, _, err := procLockFileEx.Call(
		f.Fd(),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		if err != nil && err != syscall.Errno(0) {
			return err
		}
		return syscall.EINVAL
	}
	return nil
}

// releaseOSLock unlocks the byte range on Windows.
func releaseOSLock(f *os.File) error {
	var ol syscall.Overlapped
	r1, _, err := procUnlockFileEx.Call(
		f.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		if err != nil && err != syscall.Errno(0) {
			return err
		}
		return syscall.EINVAL
	}
	return nil
}
