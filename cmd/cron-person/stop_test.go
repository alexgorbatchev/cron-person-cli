//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

// startRecorder starts `cron-person exec` for command in project, as cron
// does, and returns the live run once it is recorded.
func startRecorder(t *testing.T, state, project, command string) history.Run {
	t.Helper()
	recorder := exec.Command(os.Args[0], "--store", state, "exec", project, "--", command)
	if err := recorder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recorder.Wait() })
	log := history.New(history.DirForStore(state))
	taskID := history.TaskID(project, command)
	var run history.Run
	eventually(t, "the run to start", func() bool {
		runs, err := log.Live(taskID)
		if err == nil && len(runs) == 1 {
			run = runs[0]
			return true
		}
		return false
	})
	return run
}

func TestCLI_TaskStopEndsLiveRuns(t *testing.T) {
	for _, agent := range []string{"0", "1"} {
		t.Run("AGENT="+agent, func(t *testing.T) {
			t.Setenv("AGENT", agent)
			state, project := configProject(t)
			first := startRecorder(t, state, project, "sleep 30")
			second := startRecorder(t, state, project, "sleep 31")

			out, err := executeCommand("--store", state, "task", "stop", first.ID, second.ID)
			if err != nil {
				t.Fatalf("task stop: %v\n%s", err, out)
			}
			for _, id := range []string{first.ID, second.ID} {
				want := "[INFO] " + id + "  stopped\n"
				if agent == "1" {
					want = "id: " + id + "\tstatus: stopped\t"
				}
				if !strings.Contains(out, want) {
					t.Fatalf("output missing %q:\n%s", want, out)
				}
				run, err := history.New(history.DirForStore(state)).Get(id)
				if err != nil || run.Status != history.StatusStopped {
					t.Fatalf("run %s = %+v, %v", id, run, err)
				}
			}
		})
	}
}

func TestCLI_TaskStopReportsRunsItCannotStop(t *testing.T) {
	state, project := configProject(t)
	live := startRecorder(t, state, project, "sleep 30")
	if out, err := executeCommand("--store", state, "exec", project, "--", "true"); err != nil {
		t.Fatalf("exec: %v\n%s", err, out)
	}
	runs, err := history.New(history.DirForStore(state)).List(history.Filter{TaskID: history.TaskID(project, "true")})
	if err != nil || len(runs) != 1 {
		t.Fatalf("finished runs = %+v, %v", runs, err)
	}
	finished := runs[0].ID

	out, err := executeCommand("--store", state, "task", "stop", finished, live.ID, "20260101T000000.000000Z-00000000")
	if err == nil {
		t.Fatalf("task stop succeeded with unstoppable runs:\n%s", out)
	}
	for _, want := range []string{"run is not running: " + finished + " is succeeded", "run not found", "task history"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	if !strings.Contains(out, live.ID+"  stopped") {
		t.Fatalf("the live run was not stopped:\n%s", out)
	}
}

func TestCLI_TaskStopReportsARunThatOutlivesTheWait(t *testing.T) {
	previous := stopTimeout
	stopTimeout = 200 * time.Millisecond
	t.Cleanup(func() { stopTimeout = previous })

	state, project := configProject(t)
	release := filepath.Join(t.TempDir(), "release")
	run := startRecorder(t, state, project, "trap '' TERM; while [ ! -e "+release+" ]; do sleep 0.01; done")
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })

	out, err := executeCommand("--store", state, "task", "stop", run.ID)
	if err == nil || !strings.Contains(err.Error(), "is still running 200ms after the stop request") {
		t.Fatalf("task stop = %v\n%s", err, out)
	}
}
