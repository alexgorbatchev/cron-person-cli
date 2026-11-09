package runner

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

func TestRunnerSkipsScheduledRunsOfAPausedTask(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	log := history.New(filepath.Join(t.TempDir(), "runs"))
	if err := log.Pause(dir, "touch ran", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	err := NewRunner(st).Exec(t.Context(), ExecOptions{Dir: dir, Command: "touch ran", History: log, Trigger: history.TriggerCron})
	if err != nil {
		t.Fatalf("a skipped run failed Exec: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "ran")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("paused command executed")
	}
	run := onlyRun(t, log)
	if run.Status != history.StatusPaused || run.ExitCode != nil || !strings.HasPrefix(run.Error, "task is paused until ") {
		t.Fatalf("run = %+v", run)
	}
}

func TestRunnerRunsAPausedTaskStartedByHand(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	log := history.New(filepath.Join(t.TempDir(), "runs"))
	if err := log.Pause(dir, "touch ran", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	err := NewRunner(st).Exec(t.Context(), ExecOptions{
		Dir: dir, Command: "touch ran", History: log, Trigger: history.TriggerManual,
		OnWarning: func(msg string) { warnings = append(warnings, msg) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "ran")); statErr != nil {
		t.Fatalf("manual run of a paused task did not execute: %v", statErr)
	}
	run := onlyRun(t, log)
	if run.Status != history.StatusSucceeded || len(run.Warnings) != 1 || !strings.Contains(run.Warnings[0], "started by hand") {
		t.Fatalf("run = %+v", run)
	}
	if len(warnings) != 1 || warnings[0] != run.Warnings[0] {
		t.Fatalf("OnWarning got %q, recorded %q", warnings, run.Warnings)
	}
}

func TestRunnerRunsATaskWhosePauseEnded(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	log := history.New(filepath.Join(t.TempDir(), "runs"))
	if err := log.Pause(dir, "touch ran", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := NewRunner(st).Exec(t.Context(), ExecOptions{Dir: dir, Command: "touch ran", History: log, Trigger: history.TriggerCron}); err != nil {
		t.Fatal(err)
	}
	if run := onlyRun(t, log); run.Status != history.StatusSucceeded || len(run.Warnings) != 0 {
		t.Fatalf("run = %+v", run)
	}
}

func TestRunnerRunsATaskWhosePauseCannotBeRead(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	root := filepath.Join(t.TempDir(), "runs")
	log := history.New(root)
	if err := log.Pause(dir, "touch ran", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, history.TaskID(dir, "touch ran"), "pause.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := NewRunner(st).Exec(t.Context(), ExecOptions{Dir: dir, Command: "touch ran", History: log, Trigger: history.TriggerCron})
	if err == nil || !strings.Contains(err.Error(), "checking whether the task is paused") || strings.Contains(err.Error(), "task failed") {
		t.Fatalf("Exec error = %v, want only the pause check failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "ran")); statErr != nil {
		t.Fatalf("an unreadable pause stopped the task: %v", statErr)
	}
}

func TestRunnerSkipsAPausedTaskWhenItsRunCannotBeRecorded(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "@daily true\n")
	root := filepath.Join(t.TempDir(), "runs")
	log := history.New(root)
	if err := log.Pause(dir, "touch ran", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(root, history.TaskID(dir, "touch ran"))
	if err := os.Chmod(taskDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(taskDir, 0o700) })
	var stdout bytes.Buffer
	err := NewRunner(st).Exec(t.Context(), ExecOptions{Dir: dir, Command: "touch ran", Stdout: &stdout, History: log, Trigger: history.TriggerCron})
	if err == nil || !strings.Contains(err.Error(), "recording run") {
		t.Fatalf("Exec error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "ran")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("paused command executed")
	}
}
