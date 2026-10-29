//go:build windows

package filelock

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
	errorLockViolation      = syscall.Errno(33)
)

// Lock blocks until f holds an exclusive lock.
func Lock(f *os.File) error {
	return lockFileEx(f, lockfileExclusiveLock)
}

// TryLock acquires an exclusive lock on f without waiting, returning ErrLocked
// when another open file holds it.
func TryLock(f *os.File) error {
	err := lockFileEx(f, lockfileExclusiveLock|lockfileFailImmediately)
	if errors.Is(err, errorLockViolation) {
		return ErrLocked
	}
	return err
}

func lockFileEx(f *os.File, flags uintptr) error {
	var overlapped syscall.Overlapped
	r1, _, err := procLockFileEx.Call(
		f.Fd(),
		flags,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

// Unlock releases the lock held by f.
func Unlock(f *os.File) error {
	var overlapped syscall.Overlapped
	r1, _, err := procUnlockFileEx.Call(
		f.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}
