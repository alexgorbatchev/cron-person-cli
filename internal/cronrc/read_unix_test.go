//go:build unix

package cronrc

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	dir := filepath.Join(t.TempDir(), strings.Repeat("nested", 20))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Bind a relative name so deep checkout-owned scratch does not exceed sun_path.
	t.Chdir(dir)
	path := filepath.Join(dir, ".cronrc")
	l, err := net.Listen("unix", ".cronrc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if _, err := ReadFile(path); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("socket: %v, want ErrNotRegular", err)
	}
}
