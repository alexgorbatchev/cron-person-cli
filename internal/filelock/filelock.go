// Package filelock provides advisory, process-scoped exclusive file locks. The
// operating system releases a lock when its holder exits, so a lock can prove
// that its holder is still alive.
package filelock

import "errors"

// ErrLocked is returned by TryLock when another open file holds the lock.
var ErrLocked = errors.New("file is locked")
