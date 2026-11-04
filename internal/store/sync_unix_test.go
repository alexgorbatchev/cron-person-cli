//go:build unix

package store

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestDirectoryFlushToleratesOnlyUnsupportedFileSystems(t *testing.T) {
	for _, tt := range []struct {
		errno syscall.Errno
		fails bool
	}{
		{syscall.EINVAL, false},
		{syscall.ENOTSUP, false},
		{syscall.EOPNOTSUPP, false},
		{syscall.EIO, true},
		{syscall.ENOSPC, true},
	} {
		t.Run(tt.errno.Error(), func(t *testing.T) {
			// os.File.Sync reports a PathError wrapping the errno.
			err := unlessUnsupported(&os.PathError{Op: "sync", Path: "/state", Err: tt.errno})
			if fails := err != nil; fails != tt.fails || (fails && !errors.Is(err, tt.errno)) {
				t.Fatalf("unlessUnsupported(%v) = %v, want failure %v", tt.errno, err, tt.fails)
			}
		})
	}
	if err := syncDir(t.TempDir()); err != nil {
		t.Fatalf("flushing a directory on a local file system: %v", err)
	}
	if err := syncDir(t.TempDir() + "/missing"); err == nil {
		t.Fatal("flushing a missing directory succeeded")
	}
}
