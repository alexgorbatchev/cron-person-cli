//go:build unix

package runner

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

// FIFOs let the test swap .cronrc between the authorization read and any
// later read, which only POSIX systems provide.
func TestRunnerSingleSnapshotPreventsMidFlightReplacement(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(dir, "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cronrcPath := filepath.Join(projectDir, ".cronrc")

	fifo1 := filepath.Join(dir, "fifo1")
	fifo2 := filepath.Join(dir, "fifo2")
	for _, p := range []string{fifo1, fifo2} {
		if err := syscall.Mkfifo(p, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(fifo1, cronrcPath); err != nil {
		t.Fatal(err)
	}

	approvedSnapshot := "SHELL=/bin/sh\nAPP_VALUE=approved\n@daily true\n"
	unapprovedFile := filepath.Join(dir, "unapproved.cronrc")
	if err := os.WriteFile(unapprovedFile, []byte("SHELL=/bin/sh\nAPP_VALUE=unapproved\n@daily true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	serveFIFO := func(openPath, nextReplacement, payload string) error {
		f, err := os.OpenFile(openPath, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		if err := os.Rename(nextReplacement, cronrcPath); err != nil {
			_ = f.Close()
			return err
		}
		_, writeErr := f.WriteString(payload)
		closeErr := f.Close()
		return errors.Join(writeErr, closeErr)
	}

	writerDone := make(chan error, 1)
	go func() {
		if err := serveFIFO(cronrcPath, fifo2, approvedSnapshot); err != nil {
			writerDone <- err
			return
		}
		writerDone <- serveFIFO(cronrcPath, unapprovedFile, approvedSnapshot)
	}()

	if _, err := st.Allow(projectDir); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err = NewRunner(st).Exec(t.Context(), ExecOptions{
		Dir:     projectDir,
		Command: `printf '%s' "$APP_VALUE"`,
		Stdout:  &stdout,
	})
	if writerErr := <-writerDone; writerErr != nil {
		t.Fatalf("fifo writer error: %v", writerErr)
	}
	if err != nil {
		t.Fatalf("expected Exec to use validated snapshot, got error: %v", err)
	}
	if stdout.String() != "approved" {
		t.Fatalf("stdout=%q, want %q", stdout.String(), "approved")
	}
}
