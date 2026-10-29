//go:build windows

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Lock blocks until f holds an exclusive lock.
func Lock(f *os.File) error {
	return lockFileEx(f, windows.LOCKFILE_EXCLUSIVE_LOCK)
}

// TryLock acquires an exclusive lock on f without waiting, returning ErrLocked
// when another open file holds it.
func TryLock(f *os.File) error {
	err := lockFileEx(f, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrLocked
	}
	return err
}

// The lock covers the file's first byte, which is enough for an advisory lock
// that every holder takes the same way.
func lockFileEx(f *os.File, flags uint32) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
}

// Unlock releases the lock held by f.
func Unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}
