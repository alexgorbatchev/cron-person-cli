//go:build unix

package cronrc

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSpecialFilesAreRejectedWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "project")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	ancestor := filepath.Join(root, ".cronrc")
	if err := os.WriteFile(ancestor, []byte("@daily true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(nested, ".cronrc")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := ReadFile(fifo)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegular) {
			t.Fatalf("FIFO: %v, want ErrNotRegular", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadFile blocked on a FIFO")
	}
	if found, err := FindCronrc(nested); err != nil || found != ancestor {
		t.Fatalf("FindCronrc = %q, %v; want the regular ancestor %q past the FIFO", found, err, ancestor)
	}
}

func TestSocketIsNotRegular(t *testing.T) {
	// Unix socket paths are short, so bind under a short temporary directory.
	dir, err := os.MkdirTemp("", "sock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, ".cronrc")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if _, err := ReadFile(path); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("socket: %v, want ErrNotRegular", err)
	}
}
