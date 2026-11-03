//go:build unix

package filelock

import (
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestLockPathReportsUnlockableFiles(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only darwin refuses flock on a FIFO, which this test needs to make locking fail")
	}
	path := filepath.Join(t.TempDir(), "fifo.lock")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if f, err := LockPath(path); err == nil {
		_ = f.Close()
		t.Fatal("LockPath locked a FIFO")
	}
}
