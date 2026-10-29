//go:build windows

package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

const windowsTestGrace = time.Second

// useBash runs tasks with the POSIX shell Git for Windows provides.
func useBash(t *testing.T) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("needs bash on PATH")
	}
	t.Setenv("SHELL", bash)
}

func TestRunnerCancellationEndsTheWholeJobOnWindows(t *testing.T) {
	for _, tt := range []struct{ name, command string }{
		{"graceful stop", "(sleep 3; echo late > late) & echo ready > ready; wait"},
		{"ignores Ctrl+Break", "trap '' INT; (sleep 3; echo late > late) & echo ready > ready; wait"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			useBash(t)
			st, dir := runnerProject(t, "@daily true\n")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- NewRunner(st).Exec(ctx, ExecOptions{Dir: dir, Command: tt.command, Stdout: &bytes.Buffer{}, Grace: windowsTestGrace})
			}()
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("task never started")
				}
				time.Sleep(20 * time.Millisecond)
			}
			cancel()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "task failed") {
					t.Fatalf("Exec error = %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("cancelled task never exited")
			}
			time.Sleep(4 * time.Second)
			if _, err := os.Stat(filepath.Join(dir, "late")); err == nil {
				t.Fatal("a background process survived cancellation")
			}
		})
	}
}

func TestRunnerStopsCapturingOutputLeftOpenOnWindows(t *testing.T) {
	useBash(t)
	st, dir := runnerProject(t, "@daily true\n")
	log := history.New(filepath.Join(t.TempDir(), "runs"))
	started := time.Now()
	err := NewRunner(st).Exec(t.Context(), ExecOptions{
		Dir: dir, Command: "echo done; sleep 30 &", Stdout: &bytes.Buffer{}, History: log, Grace: windowsTestGrace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 15*time.Second {
		t.Fatalf("Exec waited %s for a background process", elapsed)
	}
	if run := onlyRun(t, log); run.Status != history.StatusSucceeded || len(run.Warnings) != 1 {
		t.Fatalf("run = %+v", run)
	}
}
