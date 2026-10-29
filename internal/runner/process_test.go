//go:build unix

package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

const testGrace = 300 * time.Millisecond

func waitForFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return strings.TrimSpace(string(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
	return ""
}

func processGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func TestRunnerStopsCapturingOutputLeftOpenByBackgroundProcesses(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	log := history.New(filepath.Join(t.TempDir(), "runs"))
	started := time.Now()
	err := NewRunner(st).Exec(t.Context(), ExecOptions{
		Dir: dir, Command: "echo done; sleep 30 &", Stdout: &bytes.Buffer{},
		History: log, Grace: testGrace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("Exec waited %s for a background process", elapsed)
	}
	run := onlyRun(t, log)
	if run.Status != history.StatusSucceeded || len(run.Warnings) != 1 || !strings.Contains(run.Warnings[0], "capture stopped") {
		t.Fatalf("run = %+v", run)
	}
}

func TestRunnerCancellationTerminatesTheWholeProcessGroup(t *testing.T) {
	for _, tt := range []struct {
		name, command string
		minWait       time.Duration
	}{
		{"terminate", "sleep 30 & echo $! > child; wait", 0},
		{"kill after grace", "trap '' TERM; sleep 30 & echo $! > child; wait", testGrace},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHELL", "/bin/sh")
			st, dir := runnerProject(t, "@daily true\n")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- NewRunner(st).Exec(ctx, ExecOptions{Dir: dir, Command: tt.command, Stdout: &bytes.Buffer{}, Grace: testGrace})
			}()
			pid, err := strconv.Atoi(waitForFile(t, filepath.Join(dir, "child")))
			if err != nil {
				t.Fatal(err)
			}
			cancelled := time.Now()
			cancel()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "task failed") {
					t.Fatalf("Exec error = %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("cancelled task never exited")
			}
			if waited := time.Since(cancelled); waited < tt.minWait {
				t.Fatalf("group killed after %s, before the %s grace period", waited, tt.minWait)
			}
			deadline := time.Now().Add(2 * time.Second)
			for !processGone(pid) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if !processGone(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				t.Fatalf("background process %d survived cancellation", pid)
			}
		})
	}
}

type brokenWriter struct{ writes int }

func (w *brokenWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("reader went away")
}

func TestRunnerKeepsRecordingAfterCallerStreamFails(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	log := history.New(filepath.Join(t.TempDir(), "runs"))
	caller := &brokenWriter{}
	var started string
	err := NewRunner(st).Exec(t.Context(), ExecOptions{
		Dir: dir, Command: "echo one; echo two; echo three", Stdout: caller, Stderr: &bytes.Buffer{},
		History: log, MaxOutputBytes: 1 << 20, Warnings: []string{"config ignored"},
		OnStart: func(id string) { started = id },
	})
	if err != nil {
		t.Fatal(err)
	}
	run := onlyRun(t, log)
	if started != run.ID || caller.writes != 1 || len(run.Warnings) != 1 || run.Warnings[0] != "config ignored" {
		t.Fatalf("started=%q writes=%d run=%+v", started, caller.writes, run)
	}
	if out := runOutput(t, log, run); out != "[out] one\n[out] two\n[out] three\n" {
		t.Fatalf("captured output = %q", out)
	}
}
