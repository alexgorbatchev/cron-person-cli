//go:build unix

package runner

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A FIFO named .cronrc would block a plain open until a writer appears.
func TestRunnerRefusesASpecialCronrcWithoutBlocking(t *testing.T) {
	st, dir := runnerProject(t, "@daily true\n")
	path := filepath.Join(dir, ".cronrc")
	if err := syscall.Unlink(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- NewRunner(st).Exec(t.Context(), ExecOptions{Dir: dir, Command: "true"}) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "no .cronrc file found") {
			t.Fatalf("Exec error = %v, want the FIFO treated as no .cronrc", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Exec blocked on a FIFO .cronrc")
	}
}
