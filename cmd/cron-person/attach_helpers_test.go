package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

// syncBuffer is a bytes.Buffer safe for the relay goroutines and the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type attachFixture struct {
	state, project string
	stdout, stderr *syncBuffer
	stdin          *io.PipeWriter
	signals        chan os.Signal
	result         chan error
}

// startAttached runs command through a foreground recorder, as `task run` does,
// with input from a pipe the test writes to and signals the test sends.
func startAttached(t *testing.T, command string, interactive bool) *attachFixture {
	t.Helper()
	state, project := configProject(t)
	inR, inW := io.Pipe()
	t.Cleanup(func() { _ = inW.Close() })
	f := &attachFixture{state: state, project: project, stdout: &syncBuffer{}, stderr: &syncBuffer{}, stdin: inW,
		signals: make(chan os.Signal, 2), result: make(chan error, 1)}
	// runAttached creates the file the recorder reports the run ID into.
	idPath := filepath.Join(t.TempDir(), "run.id")
	if err := os.WriteFile(idPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	a := attachedRun{stdin: inR, stdout: f.stdout, stderr: f.stderr, interactive: interactive, signals: f.signals, idPath: idPath}
	args := []string{"--store", state, "--config", filepath.Join(t.TempDir(), "config.json"), "exec",
		"--trigger", "manual", "--run-id-file", idPath, project, "--", command}
	go func() { f.result <- a.run(exec.Command(os.Args[0], args...)) }()
	return f
}

func (f *attachFixture) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-f.result:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("foreground run never returned")
		return nil
	}
}

func (f *attachFixture) runs(t *testing.T) []history.Run {
	t.Helper()
	runs, err := history.New(history.DirForStore(f.state)).List(history.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func (f *attachFixture) waitForRunning(t *testing.T) {
	t.Helper()
	eventually(t, "the run to start", func() bool {
		runs := f.runs(t)
		return len(runs) == 1 && runs[0].Status == history.StatusRunning && strings.Contains(f.stdout.String(), "ready")
	})
}

func (f *attachFixture) finalRun(t *testing.T) history.Run {
	t.Helper()
	var run history.Run
	eventually(t, "the run to finish", func() bool {
		runs := f.runs(t)
		if len(runs) == 1 && runs[0].Status != history.StatusRunning {
			run = runs[0]
			return true
		}
		return false
	})
	return run
}
