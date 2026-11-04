//go:build unix

package crontab

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

// A .cronrc replaced by a FIFO after approval must not stall generation,
// which runs while holding the crontab update lock.
func TestPreviewSkipsSpecialFilesWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	cronrc := filepath.Join(dir, ".cronrc")
	writeCrontabFile(t, cronrc, "@daily true\n")
	if _, err := st.Allow(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cronrc); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(cronrc, 0o600); err != nil {
		t.Fatal(err)
	}
	type result struct {
		block   string
		skipped []store.Skipped
		err     error
	}
	done := make(chan result, 1)
	go func() {
		block, _, skipped, err := Preview(st, "/opt/cron-person", testConfig)
		done <- result{block, skipped, err}
	}()
	select {
	case r := <-done:
		if r.err != nil || r.block != BlockStart+"\n"+BlockEnd || len(r.skipped) != 1 || !strings.Contains(r.skipped[0].Problem.Error(), "not a regular file") {
			t.Fatalf("block=%q skipped=%+v err=%v", r.block, r.skipped, r.err)
		}
	case <-time.After(5 * time.Second):
		_ = os.Remove(cronrc) // Unblock the stuck reader before failing.
		t.Fatal("Preview blocked on a FIFO .cronrc")
	}
}
