//go:build unix

package filelock

import (
	"errors"
	"os"
	"syscall"
)

// Lock blocks until f holds an exclusive lock.
func Lock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// TryLock acquires an exclusive lock on f without waiting, returning ErrLocked
// when another open file holds it.
func TryLock(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrLocked
	}
	return err
}

// Unlock releases the lock held by f.
func Unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
