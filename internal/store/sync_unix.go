//go:build unix

package store

import (
	"errors"
	"os"
	"syscall"
)

// syncDir flushes dir so a rename inside it survives power loss. A file
// system that cannot flush directories has nothing to flush, so that is not
// a failure.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := unlessUnsupported(f.Sync())
	closeErr := f.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// unlessUnsupported drops the errors file systems without directory flushes
// (some network and FUSE mounts) report, keeping every real failure.
func unlessUnsupported(err error) error {
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) {
		return nil
	}
	return err
}
