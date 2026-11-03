//go:build unix

package crontab

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

// A .cronrc swapped for a FIFO after its status check must not stall
// generation, which runs while holding the crontab update lock.
func TestGenerateBlockSkipsSpecialFilesWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, ".cronrc")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	type result struct {
		block string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		block, err := GenerateBlock([]store.Record{{Dir: dir, Path: fifo}}, Invocation{Bin: "/opt/cron-person", Store: filepath.Join(dir, "state.json"), Config: testConfig})
		done <- result{block, err}
	}()
	select {
	case r := <-done:
		if r.err != nil || r.block != BlockStart+"\n"+BlockEnd {
			t.Fatalf("block=%q err=%v, want an empty managed block", r.block, r.err)
		}
	case <-time.After(5 * time.Second):
		_ = os.Remove(fifo)
		t.Fatal("GenerateBlock blocked on a FIFO .cronrc")
	}
}
