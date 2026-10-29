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

type historyFixture struct {
	store, project, other string
}

// newHistoryFixture records four runs, oldest first: a cron success, a cron
// failure, a manual run in a second directory, and a cron run refused after
// the first directory's .cronrc changed.
func newHistoryFixture(t *testing.T) historyFixture {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	tmp := t.TempDir()
	f := historyFixture{
		store:   filepath.Join(tmp, "state", "allowed.json"),
		project: filepath.Join(tmp, "project"),
		other:   filepath.Join(tmp, "other"),
	}
	for _, dir := range []string{f.project, f.other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".cronrc"), []byte("@hourly true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := executeCommand("--store", f.store, "allow", dir); err != nil {
			t.Fatalf("allow: %v %s", err, out)
		}
	}
	steps := []struct {
		args    []string
		wantErr bool
	}{
		{[]string{"exec", f.project, "--", "echo line1; echo warn >&2; echo line2"}, false},
		{[]string{"exec", f.project, "--", "echo broken; exit 3"}, true},
		{[]string{"task", "run", f.other, "echo manual"}, false},
	}
	for _, s := range steps {
		if out, err := executeCommand(append([]string{"--store", f.store}, s.args...)...); (err != nil) != s.wantErr {
			t.Fatalf("%v: err=%v out=%s", s.args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(f.project, ".cronrc"), []byte("@hourly false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := executeCommand("--store", f.store, "exec", f.project, "--", "echo never"); err == nil {
		t.Fatal("exec of modified .cronrc succeeded")
	}
	return f
}

func agentField(line, key string) string {
	for _, field := range strings.Split(line, "\t") {
		if v, ok := strings.CutPrefix(field, key+": "); ok {
			return v
		}
	}
	return ""
}

func agentHistory(t *testing.T, f historyFixture, args ...string) []string {
	t.Helper()
	t.Setenv("AGENT", "1")
	out, err := executeCommand(append([]string{"--store", f.store, "task", "history"}, args...)...)
	if err != nil {
		t.Fatalf("history %v: %v %s", args, err, out)
	}
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

func TestCLI_TaskHistoryAgentMode(t *testing.T) {
	f := newHistoryFixture(t)
	lines := agentHistory(t, f)
	want := []struct{ status, exit, trigger, dir, cmd string }{
		{"blocked", "-", "cron", f.project, "echo never"},
		{"succeeded", "0", "manual", f.other, "echo manual"},
		{"failed", "3", "cron", f.project, "echo broken; exit 3"},
		{"succeeded", "0", "cron", f.project, "echo line1; echo warn >&2; echo line2"},
	}
	if len(lines) != len(want) {
		t.Fatalf("history lines = %q", lines)
	}
	for i, w := range want {
		got := []string{agentField(lines[i], "status"), agentField(lines[i], "exit"), agentField(lines[i], "trigger"), agentField(lines[i], "dir"), agentField(lines[i], "cmd")}
		if strings.Join(got, "|") != strings.Join([]string{w.status, w.exit, w.trigger, w.dir, w.cmd}, "|") {
			t.Fatalf("line %d = %q, want %+v", i, lines[i], w)
		}
		if agentField(lines[i], "task") != history.TaskID(w.dir, w.cmd) || agentField(lines[i], "id") == "" {
			t.Fatalf("line %d has wrong identifiers: %q", i, lines[i])
		}
	}

	if got := agentHistory(t, f, f.other); len(got) != 1 || agentField(got[0], "cmd") != "echo manual" {
		t.Fatalf("directory filter = %q", got)
	}
	if got := agentHistory(t, f, "--limit", "2"); len(got) != 2 || agentField(got[1], "status") != "succeeded" {
		t.Fatalf("limit = %q", got)
	}
	taskID := history.TaskID(f.project, "echo broken; exit 3")
	if got := agentHistory(t, f, "--task", taskID); len(got) != 1 || agentField(got[0], "exit") != "3" {
		t.Fatalf("task filter = %q", got)
	}
}

func TestCLI_TaskHistoryHumanMode(t *testing.T) {
	f := newHistoryFixture(t)
	t.Setenv("AGENT", "0")
	out, err := executeCommand("--store", f.store, "task", "history", "--limit", "0")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Task Runs:", "[ERROR]", "blocked", "failed (exit 3)", "[OK]", "Trigger: manual", "echo manual"} {
		if !strings.Contains(out, want) {
			t.Fatalf("human history missing %q:\n%s", want, out)
		}
	}

	empty := filepath.Join(t.TempDir(), "allowed.json")
	if out, err := executeCommand("--store", empty, "task", "history"); err != nil || out != "No recorded task runs.\n" {
		t.Fatalf("empty history = %q, %v", out, err)
	}
}

func TestCLI_TaskLog(t *testing.T) {
	f := newHistoryFixture(t)
	lines := agentHistory(t, f)
	blockedID, failedID, successID := agentField(lines[0], "id"), agentField(lines[2], "id"), agentField(lines[3], "id")

	t.Setenv("AGENT", "0")
	out, err := executeCommand("--store", f.store, "task", "log", successID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Run:        " + successID, "Directory:  " + f.project, "Status:     succeeded", "Output:\n", "[out] line1\n", "[out] line2\n", "[err] warn\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("human log missing %q:\n%s", want, out)
		}
	}
	// Separate pipes preserve order within a stream, not across streams.
	if strings.Index(out, "[out] line1") > strings.Index(out, "[out] line2") {
		t.Fatalf("stdout order lost:\n%s", out)
	}
	if out, err := executeCommand("--store", f.store, "task", "log", failedID, "--tail", "1"); err != nil || !strings.HasSuffix(out, "Output:\n[out] broken\n") {
		t.Fatalf("tail = %q, %v", out, err)
	}
	t.Setenv("HOME", filepath.Dir(f.project))
	if out, err := executeCommand("--store", f.store, "task", "log", successID); err != nil || !strings.Contains(out, "Directory:  ~/project\n") {
		t.Fatalf("home directory not folded: %q, %v", out, err)
	}
	out, err = executeCommand("--store", f.store, "task", "log", blockedID)
	if err != nil || !strings.Contains(out, "Error:      directory ~/project") || !strings.HasSuffix(out, "(no output captured)\n") {
		t.Fatalf("blocked log = %q, %v", out, err)
	}

	t.Setenv("AGENT", "1")
	out, err = executeCommand("--store", f.store, "task", "log", failedID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"id: " + failedID + "\n", "status: failed\n", "exit: 3\n", "error: task failed in ", "output:\n[out] broken\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("agent log missing %q:\n%s", want, out)
		}
	}
}

func TestCLI_TaskHistoryErrors(t *testing.T) {
	t.Setenv("AGENT", "0")
	storeFile := filepath.Join(t.TempDir(), "allowed.json")
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"task", "history", "--limit", "-1"}, "--limit must be zero or greater"},
		{[]string{"task", "log", "missing"}, "run not found: missing: run 'cron-person task history'"},
		{[]string{"task", "log", "missing", "--tail", "-1"}, "--tail must be zero or greater"},
		{[]string{"task", "log"}, "accepts 1 arg"},
	} {
		if _, err := executeCommand(append([]string{"--store", storeFile}, tt.args...)...); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("%v error = %v, want %q", tt.args, err, tt.want)
		}
	}
}

func TestCLI_TaskLogFollowsARunningTask(t *testing.T) {
	state, project := configProject(t)
	recorder := exec.Command(os.Args[0], "--store", state, "exec", project, "--", "echo early; sleep 0.5; echo late >&2; exit 2")
	if err := recorder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recorder.Wait() })
	log := history.New(history.DirForStore(state))
	var id string
	deadline := time.Now().Add(10 * time.Second)
	for id == "" && time.Now().Before(deadline) {
		if runs, err := log.List(history.Filter{}); err == nil && len(runs) == 1 && runs[0].Status == history.StatusRunning {
			id = runs[0].ID
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("run never started")
	}
	for _, agent := range []string{"0", "1"} {
		t.Setenv("AGENT", agent)
		out, err := executeCommand("--store", state, "task", "log", "--follow", id)
		if err != nil || !strings.Contains(out, "[out] early\n") || !strings.Contains(out, "[err] late\n") {
			t.Fatalf("AGENT=%s follow = %q, %v", agent, out, err)
		}
		want := "\nFinished:   failed (exit 2) after "
		if agent == "1" {
			want = "final_status: failed\nfinal_exit: 2\n"
		}
		if !strings.Contains(out, want) {
			t.Fatalf("AGENT=%s follow missing %q:\n%s", agent, want, out)
		}
	}
}
