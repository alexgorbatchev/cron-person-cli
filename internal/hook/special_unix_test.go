//go:build unix

package hook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
	// A regular .cronrc at the fixture root keeps the walk inside the fixture
	// and shows the FIFO was passed over rather than ending discovery.
	boundary := filepath.Join(dir, ".cronrc")
	if err := os.WriteFile(boundary, []byte("@daily true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- HandleHookExport(st, project, false, &stderr) }()
	select {
	case err := <-done:
		if err != nil || !strings.Contains(stderr.String(), "unauthorized .cronrc detected at "+boundary) {
			t.Fatalf("err=%v stderr=%q, want a warning for %s", err, stderr.String(), boundary)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hook export blocked on a FIFO .cronrc")
	}
}
