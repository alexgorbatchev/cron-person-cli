//go:build unix

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/display"
	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

// pauseProject is an authorized project whose .cronrc schedules `sleep 30`
// and `true`.
func pauseProject(t *testing.T) (state, project string) {
	t.Helper()
	state, project = configProject(t)
	if err := os.WriteFile(filepath.Join(project, ".cronrc"), []byte("@hourly sleep 30\n@daily true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := executeCommand("--store", state, "allow", project); err != nil {
		t.Fatalf("allow: %v %s", err, out)
	}
	return state, project
}

func TestCLI_TaskPauseStopsLiveRunsAndSkipsScheduledOnes(t *testing.T) {
	for _, agent := range []string{"0", "1"} {
		t.Run("AGENT="+agent, func(t *testing.T) {
			t.Setenv("AGENT", agent)
			state, project := pauseProject(t)
			live := startRecorder(t, state, project, "sleep 30")

			out, err := executeCommand("--store", state, "task", "pause", project, "sleep", "30", "--for", "2h")
			if err != nil {
				t.Fatalf("task pause: %v\n%s", err, out)
			}
			taskID := history.TaskID(project, "sleep 30")
			wants := []string{"[OK] Paused until ", "Task: " + display.Path(project) + "  sleep 30", "[INFO] " + live.ID + "  stopped"}
			if agent == "1" {
				wants = []string{"task: " + taskID + "\tpaused_until: ", "\tcmd: sleep 30\n", "id: " + live.ID + "\tstatus: stopped\t"}
			}
			for _, want := range wants {
				if !strings.Contains(out, want) {
					t.Fatalf("output missing %q:\n%s", want, out)
				}
			}

			var stdout, stderr bytes.Buffer
			if code := run([]string{"--store", state, "exec", project, "--", "sleep 30"}, &stdout, &stderr); code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("scheduled run of a paused task: exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
			}
			runs, err := history.New(history.DirForStore(state)).List(history.Filter{TaskID: taskID})
			if err != nil || len(runs) != 2 || runs[0].Status != history.StatusPaused || runs[1].Status != history.StatusStopped {
				t.Fatalf("runs = %+v, %v", runs, err)
			}

			list, err := executeCommand("--store", state, "task", "list")
			want := "    Task: " + taskID + "\n    Paused until: "
			if agent == "1" {
				want = "task: " + taskID + "\tschedule: @hourly\tdescription: Every hour\tpaused_until: 20"
			}
			if err != nil || !strings.Contains(list, want) {
				t.Fatalf("task list missing %q:\n%s%v", want, list, err)
			}
			if strings.Count(list, "aused_until: 20")+strings.Count(list, "Paused until: ") != 1 {
				t.Fatalf("only one task is paused:\n%s", list)
			}
			if agent == "1" && !strings.Contains(list, "paused_until: -\tcmd: true\n") {
				t.Fatalf("unpaused task not marked:\n%s", list)
			}
		})
	}
}

func TestCLI_ManualRunOfAPausedTaskWarns(t *testing.T) {
	t.Setenv("AGENT", "0")
	state, project := pauseProject(t)
	if out, err := executeCommand("--store", state, "task", "pause", project, "true", "--for", "1h"); err != nil {
		t.Fatalf("task pause: %v\n%s", err, out)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--store", state, "task", "run", project, "true"}, &stdout, &stderr); code != 0 || !strings.Contains(stderr.String(), "[WARN] task is paused until ") {
		t.Fatalf("manual run: exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
	runs, err := history.New(history.DirForStore(state)).List(history.Filter{})
	if err != nil || len(runs) != 1 || runs[0].Status != history.StatusSucceeded || len(runs[0].Warnings) != 1 {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
}

func TestCLI_TaskResumeRestoresScheduledRuns(t *testing.T) {
	for _, agent := range []string{"0", "1"} {
		t.Run("AGENT="+agent, func(t *testing.T) {
			t.Setenv("AGENT", agent)
			state, project := pauseProject(t)
			if out, err := executeCommand("--store", state, "task", "pause", project, "true", "--for", "1h"); err != nil {
				t.Fatalf("task pause: %v\n%s", err, out)
			}
			// A pause outlives its directory's authorization.
			if out, err := executeCommand("--store", state, "deny", project); err != nil {
				t.Fatalf("deny: %v\n%s", err, out)
			}
			out, err := executeCommand("--store", state, "task", "resume", project, "true")
			want := "[OK] Resumed\n     Task: " + display.Path(project) + "  true\n"
			if agent == "1" {
				want = "task: " + history.TaskID(project, "true") + "\tstatus: resumed\tcmd: true\n"
			}
			if err != nil || !strings.Contains(out, want) {
				t.Fatalf("task resume = %q, %v", out, err)
			}
			if out, err := executeCommand("--store", state, "allow", project); err != nil {
				t.Fatalf("allow: %v\n%s", err, out)
			}
			if out, err := executeCommand("--store", state, "exec", project, "--", "true"); err != nil {
				t.Fatalf("exec after resume: %v\n%s", err, out)
			}
			runs, err := history.New(history.DirForStore(state)).List(history.Filter{})
			if err != nil || len(runs) != 1 || runs[0].Status != history.StatusSucceeded {
				t.Fatalf("runs = %+v, %v", runs, err)
			}
			if _, err := executeCommand("--store", state, "task", "resume", project, "true"); err == nil || !strings.Contains(err.Error(), "task is not paused") {
				t.Fatalf("second resume = %v", err)
			}
		})
	}
}

func TestCLI_TaskPauseRefusesUnknownTasksAndBadDurations(t *testing.T) {
	state, project := pauseProject(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{project, "false", "--for", "1h"}, `no task with command "false"`},
		{[]string{project, "true", "--for", "0s"}, "--for must be a positive duration"},
		{[]string{project, "true", "--for", "soon"}, "invalid argument"},
		{[]string{project, "true"}, `required flag(s) "for" not set`},
		{[]string{other, "true", "--for", "1h"}, "only tasks in an authorized .cronrc can be paused"},
	} {
		args := append([]string{"--store", state, "task", "pause"}, tt.args...)
		if out, err := executeCommand(args...); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("task pause %v = %v\n%s", tt.args, err, out)
		}
	}
	runs := history.New(history.DirForStore(state))
	if _, paused, err := runs.PausedUntil(history.TaskID(project, "true"), time.Now()); err != nil || paused {
		t.Fatalf("a refused pause took effect: %v, %v", paused, err)
	}
}
