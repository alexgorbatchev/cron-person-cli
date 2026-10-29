package runner

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

func onlyRun(t *testing.T, log *history.Log) history.Run {
	t.Helper()
	runs, err := log.List(history.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("recorded %d runs, want 1", len(runs))
	}
	return runs[0]
}

func runOutput(t *testing.T, log *history.Log, run history.Run) string {
	t.Helper()
	var buf bytes.Buffer
	if err := log.WriteOutput(&buf, run, 0); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestRunnerRecordsCompletedRuns(t *testing.T) {
	for _, tt := range []struct {
		name, command string
		status        history.Status
		exitCode      int
	}{
		{"success", "echo hello; echo oops >&2", history.StatusSucceeded, 0},
		{"failure", "echo hello; echo oops >&2; exit 4", history.StatusFailed, 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHELL", "/bin/sh")
			st, dir := runnerProject(t, "@daily true\n")
			log := history.New(filepath.Join(t.TempDir(), "runs"))
			var stdout, stderr bytes.Buffer
			err := NewRunner(st).Exec(t.Context(), ExecOptions{
				Dir: dir, Command: tt.command, Stdout: &stdout, Stderr: &stderr,
				History: log, Trigger: history.TriggerManual,
			})
			var exitErr *exec.ExitError
			if tt.exitCode == 0 && err != nil || tt.exitCode != 0 && !errors.As(err, &exitErr) {
				t.Fatalf("Exec error = %v", err)
			}
			if stdout.String() != "hello\n" || stderr.String() != "oops\n" {
				t.Fatalf("caller streams lost output: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			run := onlyRun(t, log)
			if run.Status != tt.status || run.ExitCode == nil || *run.ExitCode != tt.exitCode ||
				run.Dir != dir || run.Command != tt.command || run.Trigger != history.TriggerManual {
				t.Fatalf("run = %+v", run)
			}
			if out := runOutput(t, log, run); !strings.Contains(out, "[out] hello\n") || !strings.Contains(out, "[err] oops\n") {
				t.Fatalf("captured output = %q", out)
			}
		})
	}
}

func TestRunnerRecordsBlockedRuns(t *testing.T) {
	st, dir := runnerProject(t, "@daily true\n")
	if err := os.WriteFile(filepath.Join(dir, ".cronrc"), []byte("@daily false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	log := history.New(filepath.Join(t.TempDir(), "runs"))
	err := NewRunner(st).Exec(t.Context(), ExecOptions{Dir: dir, Command: "touch ran", History: log, Trigger: history.TriggerCron})
	if err == nil || !strings.Contains(err.Error(), "changed .cronrc") {
		t.Fatalf("Exec error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "ran")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("blocked command executed")
	}
	run := onlyRun(t, log)
	if run.Status != history.StatusBlocked || run.ExitCode != nil || !strings.Contains(run.Error, "changed .cronrc") {
		t.Fatalf("run = %+v", run)
	}
}

func TestRunnerRunsTaskWhenHistoryUnavailable(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	root := filepath.Join(t.TempDir(), "runs")
	if err := os.WriteFile(root, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := NewRunner(st).Exec(t.Context(), ExecOptions{Dir: dir, Command: "echo ran", Stdout: &stdout, History: history.New(root)})
	if err == nil || !strings.Contains(err.Error(), "recording run") || strings.Contains(err.Error(), "task failed") {
		t.Fatalf("Exec error = %v, want only the history failure", err)
	}
	if stdout.String() != "ran\n" {
		t.Fatalf("task did not run: stdout=%q", stdout.String())
	}
}

func TestRunnerSerializesSharedCallerWriter(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	var combined bytes.Buffer
	err := NewRunner(st).Exec(t.Context(), ExecOptions{
		Dir: dir, Command: "for i in 1 2 3 4 5; do echo out$i; echo err$i >&2; done",
		Stdout: &combined, Stderr: &combined, History: history.New(filepath.Join(t.TempDir(), "runs")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(combined.String(), "\n"); got != 10 {
		t.Fatalf("combined output lost writes: %q", combined.String())
	}
}

func TestRunnerEnforcesRetentionAfterEachRun(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	log := history.New(filepath.Join(t.TempDir(), "runs"))
	opts := ExecOptions{Dir: dir, Command: "echo run", Stdout: &bytes.Buffer{}, History: log, Retention: history.Retention{MaxBytes: 1}}
	for range 3 {
		if err := NewRunner(st).Exec(t.Context(), opts); err != nil {
			t.Fatal(err)
		}
	}
	if runs, err := log.List(history.Filter{}); err != nil || len(runs) != 1 {
		t.Fatalf("retained %d runs (%v), want only the latest", len(runs), err)
	}
}

func TestRunnerReportsPruneFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	root := filepath.Join(t.TempDir(), "runs")
	log := history.New(root)
	// An older run of another task whose lock cannot be probed blocks pruning.
	var lockPath string
	for range 2 {
		rec, err := log.Start(history.StartOptions{Dir: "/srv/other", Command: "job", Trigger: history.TriggerCron})
		if err != nil {
			t.Fatal(err)
		}
		if err := rec.Finish(nil); err != nil {
			t.Fatal(err)
		}
		if lockPath == "" {
			lockPath = filepath.Join(root, history.TaskID("/srv/other", "job"), rec.ID(), "run.lock")
		}
	}
	if err := os.Chmod(lockPath, 0); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := NewRunner(st).Exec(t.Context(), ExecOptions{Dir: dir, Command: "echo ran", Stdout: &stdout, History: log, Retention: history.Retention{MaxBytes: 1}})
	if err == nil || !strings.Contains(err.Error(), "pruning run history") || stdout.String() != "ran\n" {
		t.Fatalf("Exec error = %v, stdout = %q", err, stdout.String())
	}
}
