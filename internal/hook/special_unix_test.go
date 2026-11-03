//go:build unix

package hook

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

// The prompt hook runs on every prompt, so a FIFO named .cronrc must never
// make it wait.
func TestHookExportIgnoresSpecialCronrcWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(dir, "allowed.json"))
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(dir, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(project, ".cronrc"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- HandleHookExport(st, project, false, &stderr) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hook export blocked on a FIFO .cronrc")
	}
}
