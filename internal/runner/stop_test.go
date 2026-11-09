//go:build unix

package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

const testStopPoll = 10 * time.Millisecond

// liveRun waits until log holds exactly one run, still running, and returns it.
func liveRun(t *testing.T, log *history.Log) history.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runs, err := log.List(history.Filter{}); err == nil && len(runs) == 1 && runs[0].Status == history.StatusRunning {
			return runs[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run never started")
	return history.Run{}
}

func TestRunnerStopsTheTaskOnRequest(t *testing.T) {
	for _, tt := range []struct {
		name, command string
		minWait       time.Duration
		exitCode      *int
	}{
		{"terminate", "sleep 30 & echo $! > child; wait", 0, nil},
		{"handled terminate", "trap 'exit 5' TERM; sleep 30 & echo $! > child; wait", 0, func() *int { c := 5; return &c }()},
		{"kill after grace", "trap '' TERM; sleep 30 & echo $! > child; wait", testGrace, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHELL", "/bin/sh")
			st, dir := runnerProject(t, "@daily true\n")
			log := history.New(filepath.Join(t.TempDir(), "runs"))
			done := make(chan error, 1)
			go func() {
				done <- NewRunner(st).Exec(t.Context(), ExecOptions{
					Dir: dir, Command: tt.command, Stdout: &bytes.Buffer{}, History: log,
					Grace: testGrace, StopPoll: testStopPoll,
				})
			}()
			pid, err := strconv.Atoi(waitForFile(t, filepath.Join(dir, "child")))
			if err != nil {
				t.Fatal(err)
			}
			run := liveRun(t, log)
			requested := time.Now()
			if _, err := log.RequestStop(run.ID); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("a requested stop failed Exec: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("stopped task never exited")
			}
			if waited := time.Since(requested); waited < tt.minWait {
				t.Fatalf("group killed after %s, before the %s grace period", waited, tt.minWait)
			}
			final := onlyRun(t, log)
			if final.Status != history.StatusStopped || final.Error != "stopped on request" {
				t.Fatalf("run = %+v", final)
			}
			if (final.ExitCode == nil) != (tt.exitCode == nil) || (final.ExitCode != nil && *final.ExitCode != *tt.exitCode) {
				t.Fatalf("exit code = %v, want %v", final.ExitCode, tt.exitCode)
			}
			deadline := time.Now().Add(2 * time.Second)
			for !processGone(pid) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if !processGone(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				t.Fatalf("background process %d survived the stop", pid)
			}
		})
	}
}

func TestRunnerReportsAFailedStopWatchAfterTheTask(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	root := filepath.Join(t.TempDir(), "runs")
	log := history.New(root)
	done := make(chan error, 1)
	go func() {
		done <- NewRunner(st).Exec(t.Context(), ExecOptions{
			Dir: dir, Command: "touch started; while [ ! -e release ]; do sleep 0.01; done", Stdout: &bytes.Buffer{},
			History: log, Grace: testGrace, StopPoll: testStopPoll,
		})
	}()
	run := liveRun(t, log)
	runDir := filepath.Join(root, run.TaskID, run.ID)
	if err := os.Chmod(runDir, 0); err != nil {
		t.Fatal(err)
	}
	// Give the watcher a few polls to fail, then let the task finish.
	time.Sleep(10 * testStopPoll)
	if err := os.Chmod(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "watching for a stop request") {
			t.Fatalf("Exec error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("task never exited")
	}
	if final := onlyRun(t, log); final.Status != history.StatusSucceeded {
		t.Fatalf("a failed watch changed the outcome: %+v", final)
	}
}
