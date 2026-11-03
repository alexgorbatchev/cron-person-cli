// Package filelock provides advisory, process-scoped exclusive file locks. The
// operating system releases a lock when its holder exits, so a lock can prove
// that its holder is still alive.
package filelock

import (
	"errors"
	"os"
)

// ErrLocked is returned by TryLock when another open file holds the lock.
var ErrLocked = errors.New("file is locked")

// LockPath opens the lock file at path, creating it when missing, and blocks
// until it holds an exclusive lock on it. Call release to unlock explicitly
// before closing, because Windows may hold a lock for a while after its file
// is closed.
func LockPath(path string) (release func() error, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := Lock(f); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return func() error { return errors.Join(Unlock(f), f.Close()) }, nil
}
