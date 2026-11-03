//go:build unix

package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
)

func TestSpecialAndOversizedCronrcCannotBeAuthorized(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(filepath.Join(dir, "allowed.json"))
	if err != nil {
		t.Fatal(err)
	}

	fifoDir := filepath.Join(dir, "fifo")
	if err := os.Mkdir(fifoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(fifoDir, ".cronrc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Allow(fifoDir); !errors.Is(err, cronrc.ErrNotRegular) {
		t.Fatalf("Allow(FIFO) = %v, want ErrNotRegular", err)
	}
	if status, _, err := st.Status(fifoDir); err != nil || status != StatusMissingCronrc {
		t.Fatalf("Status(FIFO) = %s, %v; want %s", status, err, StatusMissingCronrc)
	}

	bigDir := filepath.Join(dir, "big")
	if err := os.Mkdir(bigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(bigDir, ".cronrc")
	writeStoreFile(t, big, "@daily true\n")
	if _, err := st.Allow(bigDir); err != nil {
		t.Fatal(err)
	}
	writeStoreFile(t, big, strings.Repeat("#", cronrc.MaxSize+1))
	if _, err := st.Allow(bigDir); !errors.Is(err, cronrc.ErrTooLarge) {
		t.Fatalf("Allow(oversized) = %v, want ErrTooLarge", err)
	}
	if _, _, err := st.Status(bigDir); !errors.Is(err, cronrc.ErrTooLarge) {
		t.Fatalf("Status(oversized) = %v, want ErrTooLarge", err)
	}
}
