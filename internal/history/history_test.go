package history

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func readMeta(t *testing.T, l *Log, id string) Run {
	t.Helper()
	run, err := l.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func readOutput(t *testing.T, l *Log, run Run, tail int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := l.WriteOutput(&buf, run, tail); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func commandError(t *testing.T, script string) error {
	t.Helper()
	err := exec.Command("/bin/sh", "-c", script).Run()
	if err == nil {
		t.Fatalf("script %q unexpectedly succeeded", script)
	}
	return err
}

func startRun(t *testing.T, l *Log, dir, command string) *Recording {
	t.Helper()
	rec, err := l.Start(StartOptions{Dir: dir, Command: command, Trigger: TriggerCron})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestTaskIDAndStoreDir(t *testing.T) {
	a := TaskID("/srv/app", "./backup.sh")
	if a != TaskID("/srv/app", "./backup.sh") || len(a) != 2*taskIDBytes {
		t.Fatalf("unstable or malformed task id %q", a)
	}
	for _, other := range []string{TaskID("/srv/app", "./backup.sh --full"), TaskID("/srv/other", "./backup.sh")} {
		if other == a {
			t.Fatalf("task ids collide for different inputs: %q", a)
		}
	}
	if got := DirForStore("/state/cron-person/allowed.json"); got != "/state/cron-person/runs" {
		t.Fatalf("DirForStore = %q", got)
	}
}

func TestRecordingOutcomes(t *testing.T) {
	code := func(n int) *int { return &n }
	for _, tt := range []struct {
		name     string
		finish   func(*Recording) error
		status   Status
		exitCode *int
		errText  string
	}{
		{"success", func(r *Recording) error { return r.Finish(nil) }, StatusSucceeded, code(0), ""},
		{"exit status", func(r *Recording) error { return r.Finish(commandError(t, "exit 7")) }, StatusFailed, code(7), "exit status 7"},
		{"signal", func(r *Recording) error { return r.Finish(commandError(t, "kill -9 $$")) }, StatusFailed, nil, "signal: killed"},
		{"start failure", func(r *Recording) error { return r.Finish(errors.New("shell missing")) }, StatusFailed, nil, "shell missing"},
		{"blocked", func(r *Recording) error { return r.Reject(errors.New("directory is unauthorized")) }, StatusBlocked, nil, "directory is unauthorized"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := New(t.TempDir())
			rec := startRun(t, l, "/srv/app", "./job")
			if running := readMeta(t, l, rec.ID()); running.Status != StatusRunning {
				t.Fatalf("in-progress run = %+v", running)
			}
			if err := tt.finish(rec); err != nil {
				t.Fatal(err)
			}
			run := readMeta(t, l, rec.ID())
			if run.Status != tt.status || run.Error != tt.errText || run.TaskID != TaskID("/srv/app", "./job") || run.Trigger != TriggerCron {
				t.Fatalf("run = %+v", run)
			}
			if (run.ExitCode == nil) != (tt.exitCode == nil) || (run.ExitCode != nil && *run.ExitCode != *tt.exitCode) {
				t.Fatalf("exit code = %v, want %v", run.ExitCode, tt.exitCode)
			}
			if run.FinishedAt.Before(run.StartedAt) || run.Duration() < 0 {
				t.Fatalf("invalid timing: %+v", run)
			}
		})
	}
}

func TestRecordingCapturesInterleavedStreams(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/app", "./job")
	long := strings.Repeat("x", maxPartialLine+10)
	writes := []struct {
		stderr bool
		data   string
	}{
		{false, "first\nsec"},
		{true, "warn\n"},
		{false, "ond\n"},
		{true, long + "\n"},
		{false, "no newline"},
	}
	for _, w := range writes {
		target := rec.Stdout()
		if w.stderr {
			target = rec.Stderr()
		}
		if n, err := target.Write([]byte(w.data)); err != nil || n != len(w.data) {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	if err := rec.Finish(nil); err != nil {
		t.Fatal(err)
	}

	run := readMeta(t, l, rec.ID())
	want := strings.Join([]string{
		"[out] first",
		"[err] warn",
		"[out] second",
		"[err] " + long[:maxPartialLine],
		"[err] " + long[maxPartialLine:],
		"[out] no newline",
		"",
	}, "\n")
	if got := readOutput(t, l, run, 0); got != want {
		t.Fatalf("output mismatch:\n%.200q\nwant\n%.200q", got, want)
	}
	if got := readOutput(t, l, run, 2); got != "[err] "+long[maxPartialLine:]+"\n[out] no newline\n" {
		t.Fatalf("tail = %q", got)
	}
	if got := readOutput(t, l, run, 100); got != want {
		t.Fatalf("tail larger than output = %.200q", got)
	}
}

func TestRecordingFlushesUnterminatedOutputAtLimit(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/app", "./job")
	chunk := strings.Repeat("y", maxPartialLine)
	if _, err := rec.Stdout().Write([]byte(chunk)); err != nil {
		t.Fatal(err)
	}
	if rec.stdout.buf == nil || len(rec.stdout.buf) != 0 {
		t.Fatalf("partial line was not flushed at the buffer limit: %d bytes held", len(rec.stdout.buf))
	}
	if err := rec.Finish(nil); err != nil {
		t.Fatal(err)
	}
	if got := readOutput(t, l, readMeta(t, l, rec.ID()), 0); got != "[out] "+chunk+"\n" {
		t.Fatalf("output = %.80q", got)
	}
}

func TestRecordingReportsCaptureFailures(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/app", "./job")
	if err := rec.output.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := rec.Stdout().Write([]byte("lost\n")); err != nil {
			t.Fatalf("capture failure leaked into the task stream: %v", err)
		}
	}
	err := rec.Finish(nil)
	if err == nil || !strings.Contains(err.Error(), "writing run output") || !strings.Contains(err.Error(), rec.ID()) {
		t.Fatalf("Finish error = %v", err)
	}
	if run := readMeta(t, l, rec.ID()); run.Status != StatusSucceeded {
		t.Fatalf("task outcome should still be recorded, got %+v", run)
	}
}

func TestStartFailures(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, root string)
		want  string
	}{
		{"root is a file", func(t *testing.T, root string) {
			if err := os.WriteFile(root, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "creating run directory"},
		{"task record is a directory", func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, TaskID("/srv/app", "./job"), taskFile), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "committing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "runs")
			tt.setup(t, root)
			if _, err := New(root).Start(StartOptions{Dir: "/srv/app", Command: "./job", Trigger: TriggerManual}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Start error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestListFiltersAndOrdersRuns(t *testing.T) {
	l := New(t.TempDir())
	if runs, err := l.List(Filter{}); err != nil || len(runs) != 0 {
		t.Fatalf("empty history = %v, %v", runs, err)
	}
	specs := []struct{ dir, command string }{
		{"/srv/a", "one"}, {"/srv/b", "two"}, {"/srv/a", "three"}, {"/srv/a", "one"},
	}
	var ids []string
	for _, s := range specs {
		rec := startRun(t, l, s.dir, s.command)
		if err := rec.Finish(nil); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.ID())
	}
	reversed := func(idx ...int) []string {
		out := make([]string, 0, len(idx))
		for i := len(idx) - 1; i >= 0; i-- {
			out = append(out, ids[idx[i]])
		}
		return out
	}
	for _, tt := range []struct {
		name   string
		filter Filter
		want   []string
	}{
		{"all newest first", Filter{}, reversed(0, 1, 2, 3)},
		{"limit", Filter{Limit: 2}, reversed(2, 3)},
		{"directory", Filter{Dir: "/srv/a"}, reversed(0, 2, 3)},
		{"task", Filter{TaskID: TaskID("/srv/a", "one")}, reversed(0, 3)},
		{"directory and task mismatch", Filter{Dir: "/srv/b", TaskID: TaskID("/srv/a", "one")}, []string{}},
		{"unknown directory", Filter{Dir: "/srv/none"}, []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runs, err := l.List(tt.filter)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(runs))
			for _, r := range runs {
				got = append(got, r.ID)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("ids = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListAndGetReportCorruptRecords(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")
	if err := rec.Finish(nil); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(l.root, rec.run.TaskID)
	if err := os.WriteFile(filepath.Join(taskDir, rec.ID(), metaFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.List(Filter{}); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("List error = %v", err)
	}
	if _, err := l.Get(rec.ID()); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("Get error = %v", err)
	}
	if err := os.Remove(filepath.Join(taskDir, taskFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := l.List(Filter{Dir: "/srv/a"}); err == nil || !strings.Contains(err.Error(), "reading") {
		t.Fatalf("List by dir error = %v", err)
	}
}

func TestGetRejectsUnknownAndUnsafeIDs(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")
	if err := rec.Finish(nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", ".", "..", "missing", "../" + rec.ID(), rec.run.TaskID + "/" + rec.ID()} {
		if _, err := l.Get(id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(%q) error = %v, want ErrNotFound", id, err)
		}
	}
	if _, err := New(filepath.Join(t.TempDir(), "absent")).Get("x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty history = %v", err)
	}
	if err := l.WriteOutput(&bytes.Buffer{}, Run{TaskID: "none", ID: "none"}, 0); err == nil {
		t.Fatal("expected missing output error")
	}
}

// TestHelperAbandonRun is re-executed as a child process by
// TestCrashedRecorderReadsAsInterrupted. It starts a run and exits without
// recording an outcome, as a killed cron-person would.
func TestHelperAbandonRun(t *testing.T) {
	root := os.Getenv("HISTORY_HELPER_ROOT")
	if root == "" {
		t.Skip("helper process only")
	}
	rec, err := New(root).Start(StartOptions{Dir: "/srv/a", Command: "one", Trigger: TriggerCron})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Print(rec.ID())
	os.Exit(0)
}

func TestCrashedRecorderReadsAsInterrupted(t *testing.T) {
	root := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^TestHelperAbandonRun$")
	child.Env = append(os.Environ(), "HISTORY_HELPER_ROOT="+root)
	out, err := child.Output()
	if err != nil {
		t.Fatalf("helper failed: %v", err)
	}
	if run := readMeta(t, New(root), string(out)); run.Status != StatusInterrupted || run.Duration() != 0 {
		t.Fatalf("abandoned run = %+v", run)
	}
}

func TestLiveRecorderReadsAsRunning(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")
	if run := readMeta(t, l, rec.ID()); run.Status != StatusRunning {
		t.Fatalf("live run = %+v", run)
	}
	if err := rec.Finish(nil); err != nil {
		t.Fatal(err)
	}
	if run := readMeta(t, l, rec.ID()); run.Status != StatusSucceeded {
		t.Fatalf("finished run = %+v", run)
	}
	if err := os.Remove(filepath.Join(rec.dir, lockFile)); err != nil {
		t.Fatal(err)
	}
	if run := readMeta(t, l, rec.ID()); run.Status != StatusSucceeded {
		t.Fatalf("finished run without lock file = %+v", run)
	}
}

func TestReadersAndPruningHandleIncompleteRuns(t *testing.T) {
	l := New(t.TempDir())
	rec := finishedRun(t, l, "/srv/a", "one", "")
	taskDir := filepath.Join(l.root, rec.run.TaskID)
	now := time.Now().UTC()
	// Run directories without metadata, as a recorder leaves one in the instant
	// before it takes the lock or after crashing there. Both predate the finished
	// run, so only the creation grace can protect them.
	young, err := newRunID(rec.run.StartedAt.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	abandoned, err := newRunID(rec.run.StartedAt.Add(-2 * creationGrace))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{young, abandoned, "notes"} {
		if err := os.MkdirAll(filepath.Join(taskDir, id), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := l.List(Filter{})
	if err != nil || len(runs) != 1 || runs[0].ID != rec.ID() {
		t.Fatalf("List = %v, %v", runs, err)
	}
	for _, id := range []string{young, "notes"} {
		if _, err := l.Get(id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(%s) = %v", id, err)
		}
	}
	if err := l.Prune(Retention{MaxBytes: 1}, now); err != nil {
		t.Fatal(err)
	}
	for id, wantKept := range map[string]bool{young: true, abandoned: false, "notes": true, rec.ID(): true} {
		_, err := os.Stat(filepath.Join(taskDir, id))
		if kept := err == nil; kept != wantKept {
			t.Fatalf("%s kept = %v, want %v", id, kept, wantKept)
		}
	}
}

func finishedRun(t *testing.T, l *Log, dir, command, output string) *Recording {
	t.Helper()
	rec := startRun(t, l, dir, command)
	if _, err := rec.Stdout().Write([]byte(output)); err != nil {
		t.Fatal(err)
	}
	if err := rec.Finish(nil); err != nil {
		t.Fatal(err)
	}
	return rec
}

func remainingIDs(t *testing.T, l *Log) []string {
	t.Helper()
	runs, err := l.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestPrune(t *testing.T) {
	big := strings.Repeat("z", 4096) + "\n"
	type fixture struct {
		l                                         *Log
		old1, old2, latestA, live, latestB, total int64
		ids                                       map[string]string
	}
	setup := func(t *testing.T) fixture {
		l := New(t.TempDir())
		ids := map[string]string{
			"old1":    finishedRun(t, l, "/srv/a", "one", big).ID(),
			"old2":    finishedRun(t, l, "/srv/a", "one", big).ID(),
			"latestA": finishedRun(t, l, "/srv/a", "one", big).ID(),
		}
		liveRec := startRun(t, l, "/srv/b", "two")
		if _, err := liveRec.Stdout().Write([]byte(big)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = liveRec.Finish(nil) })
		ids["live"] = liveRec.ID()
		// The live run is not its task's newest, so only its lock protects it.
		ids["latestB"] = finishedRun(t, l, "/srv/b", "two", "").ID()
		f := fixture{l: l, ids: ids}
		size := func(id string) int64 {
			run := readMeta(t, l, id)
			n, err := dirSize(l.runDir(runRef{taskID: run.TaskID, runID: id}))
			if err != nil {
				t.Fatal(err)
			}
			f.total += n
			return n
		}
		f.old1, f.old2, f.latestA, f.live, f.latestB = size(ids["old1"]), size(ids["old2"]), size(ids["latestA"]), size(ids["live"]), size(ids["latestB"])
		return f
	}
	for _, tt := range []struct {
		name      string
		retention func(f fixture) Retention
		later     time.Duration
		removed   []string
	}{
		{"disabled", func(fixture) Retention { return Retention{} }, 1000 * time.Hour, nil},
		{"age keeps young runs", func(fixture) Retention { return Retention{MaxAge: time.Hour} }, 0, nil},
		{"age removes old finished runs", func(fixture) Retention { return Retention{MaxAge: time.Hour} }, 2 * time.Hour, []string{"old1", "old2"}},
		{"size within limit", func(f fixture) Retention { return Retention{MaxBytes: f.total} }, 0, nil},
		{"size removes oldest first", func(f fixture) Retention { return Retention{MaxBytes: f.total - f.old1} }, 0, []string{"old1"}},
		{"size never removes latest or live runs", func(fixture) Retention { return Retention{MaxBytes: 1} }, 0, []string{"old1", "old2"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t)
			if err := f.l.Prune(tt.retention(f), time.Now().Add(tt.later)); err != nil {
				t.Fatal(err)
			}
			var want []string
			for name, id := range f.ids {
				if !slices.Contains(tt.removed, name) {
					want = append(want, id)
				}
			}
			slices.Sort(want)
			if got := remainingIDs(t, f.l); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("remaining = %v, want %v (ids %v)", got, want, f.ids)
			}
		})
	}
}

func TestPruneRemovesInterruptedRunsAndKeepsForeignDirectories(t *testing.T) {
	l := New(t.TempDir())
	abandoned := startRun(t, l, "/srv/a", "one")
	if err := abandoned.lock.Close(); err != nil {
		t.Fatal(err)
	}
	latest := finishedRun(t, l, "/srv/a", "one", "")
	foreign := filepath.Join(l.root, latest.run.TaskID, "notes")
	if err := os.MkdirAll(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := l.Prune(Retention{MaxAge: time.Minute}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Get(abandoned.ID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("interrupted run survived: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("foreign directory removed: %v", err)
	}
	if _, err := l.Get(latest.ID()); err != nil {
		t.Fatal(err)
	}
}

func TestPruneReportsRemovalFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	l := New(t.TempDir())
	old := finishedRun(t, l, "/srv/a", "one", "")
	finishedRun(t, l, "/srv/a", "one", "")
	taskDir := filepath.Join(l.root, old.run.TaskID)
	if err := os.Chmod(taskDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(taskDir, 0o700) })
	err := l.Prune(Retention{MaxBytes: 1}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "pruning run "+old.ID()) {
		t.Fatalf("Prune error = %v", err)
	}
}

func TestFilesystemFailuresAreReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	lockOf := func(r *Recording) string { return filepath.Join(r.dir, lockFile) }
	for _, tt := range []struct {
		name string
		// deny returns the path whose permissions are removed.
		deny func(l *Log, live *Recording) string
		op   func(l *Log, live *Recording) error
		want string
	}{
		{"list unreadable root", func(l *Log, _ *Recording) string { return l.root },
			func(l *Log, _ *Recording) error { _, err := l.List(Filter{}); return err }, "reading history"},
		{"get unreadable root", func(l *Log, _ *Recording) string { return l.root },
			func(l *Log, r *Recording) error { _, err := l.Get(r.ID()); return err }, "reading history"},
		{"list unreadable task", func(l *Log, r *Recording) string { return filepath.Join(l.root, r.run.TaskID) },
			func(l *Log, _ *Recording) error { _, err := l.List(Filter{}); return err }, "reading task history"},
		{"probe unreadable lock", func(_ *Log, r *Recording) string { return lockOf(r) },
			func(l *Log, r *Recording) error { _, err := l.Get(r.ID()); return err }, "opening run lock"},
		{"prune unreadable lock", func(_ *Log, r *Recording) string { return lockOf(r) },
			func(l *Log, _ *Recording) error { return l.Prune(Retention{MaxBytes: 1}, time.Now()) }, "opening run lock"},
		{"prune unreadable run", func(_ *Log, r *Recording) string { return r.dir },
			func(l *Log, _ *Recording) error { return l.Prune(Retention{MaxBytes: 1}, time.Now()) }, "permission denied"},
		{"prune unreadable root", func(l *Log, _ *Recording) string { return l.root },
			func(l *Log, _ *Recording) error { return l.Prune(Retention{MaxAge: time.Hour}, time.Now()) }, "reading history"},
		{"start in read-only task", func(l *Log, r *Recording) string { return filepath.Join(l.root, r.run.TaskID) },
			func(l *Log, _ *Recording) error {
				_, err := l.Start(StartOptions{Dir: "/srv/a", Command: "one", Trigger: TriggerCron})
				return err
			}, "permission denied"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := New(t.TempDir())
			live := startRun(t, l, "/srv/a", "one")
			finishedRun(t, l, "/srv/a", "one", "")
			path := tt.deny(l, live)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			mode := os.FileMode(0)
			if info.IsDir() && strings.HasPrefix(tt.name, "start") {
				mode = 0o500
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, info.Mode().Perm()) })
			if err := tt.op(l, live); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestTailRejectsOversizedRecords(t *testing.T) {
	l := New(t.TempDir())
	rec := finishedRun(t, l, "/srv/a", "one", "")
	run := readMeta(t, l, rec.ID())
	huge := strings.Repeat("q", 2*maxPartialLine)
	if err := os.WriteFile(l.outputPath(run.TaskID, run.ID), []byte(huge+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := l.WriteOutput(&bytes.Buffer{}, run, 1); err == nil || !strings.Contains(err.Error(), "reading run output") {
		t.Fatalf("WriteOutput error = %v", err)
	}
}

func TestRecordingTruncatesOutputAtLimit(t *testing.T) {
	l := New(t.TempDir())
	rec, err := l.Start(StartOptions{Dir: "/srv/a", Command: "one", Trigger: TriggerCron, MaxOutputBytes: 30})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"first\n", "second\n", "third\n", "fourth\n"} {
		if _, err := rec.Stdout().Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	rec.Warn("something odd")
	if err := rec.Finish(nil); err != nil {
		t.Fatal(err)
	}
	run := readMeta(t, l, rec.ID())
	want := "[out] first\n[out] second\n[cron-person] output truncated after 30 bytes\n"
	if got := readOutput(t, l, run, 0); got != want || !run.Truncated || fmt.Sprint(run.Warnings) != "[something odd]" {
		t.Fatalf("output %q truncated=%v warnings=%v", got, run.Truncated, run.Warnings)
	}
}

func TestFollowStreamsUntilTheRunFinishes(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")
	if _, err := rec.Stdout().Write([]byte("early\nmore\n")); err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	type result struct {
		run Run
		err error
	}
	done := make(chan result, 1)
	go func() {
		run, err := l.Follow(t.Context(), pw, readMeta(t, l, rec.ID()), 1, 5*time.Millisecond)
		pw.Close()
		done <- result{run, err}
	}()
	sc := bufio.NewScanner(pr)
	expect := func(want string) {
		t.Helper()
		if !sc.Scan() || sc.Text() != want {
			t.Fatalf("followed line = %q, want %q (%v)", sc.Text(), want, sc.Err())
		}
	}
	expect("[out] more")
	if _, err := rec.Stderr().Write([]byte("late\n")); err != nil {
		t.Fatal(err)
	}
	expect("[err] late")
	if _, err := rec.Stdout().Write([]byte("final")); err != nil {
		t.Fatal(err)
	}
	if err := rec.Finish(commandError(t, "exit 4")); err != nil {
		t.Fatal(err)
	}
	expect("[out] final")
	if sc.Scan() {
		t.Fatalf("unexpected line %q", sc.Text())
	}
	res := <-done
	if res.err != nil || res.run.Status != StatusFailed || *res.run.ExitCode != 4 {
		t.Fatalf("Follow = %+v, %v", res.run, res.err)
	}
}

func TestFollowStopsOnCancellationAndMissingOutput(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")
	t.Cleanup(func() { _ = rec.Finish(nil) })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := l.Follow(ctx, io.Discard, readMeta(t, l, rec.ID()), 0, time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("Follow after cancel = %v", err)
	}
	if _, err := l.Follow(t.Context(), io.Discard, Run{TaskID: "none", ID: "none"}, 0, time.Millisecond); err == nil {
		t.Fatal("expected missing output error")
	}
}
