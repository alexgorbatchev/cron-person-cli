package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRequestStopMarksOnlyLiveRuns(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")

	if requested, err := rec.StopRequested(); err != nil || requested {
		t.Fatalf("before request: %v, %v", requested, err)
	}
	run, err := l.RequestStop(rec.ID())
	if err != nil || run.ID != rec.ID() || run.Status != StatusRunning {
		t.Fatalf("RequestStop = %+v, %v", run, err)
	}
	if requested, err := rec.StopRequested(); err != nil || !requested {
		t.Fatalf("after request: %v, %v", requested, err)
	}
	// Asking again while the recorder is still stopping is harmless.
	if _, err := l.RequestStop(rec.ID()); err != nil {
		t.Fatal(err)
	}

	if err := rec.Finish(nil); err != nil {
		t.Fatal(err)
	}
	if run, err := l.RequestStop(rec.ID()); !errors.Is(err, ErrNotRunning) || run.Status != StatusSucceeded {
		t.Fatalf("finished run: %+v, %v", run, err)
	}
	if _, err := l.RequestStop("20260101T000000.000000Z-00000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
}

func TestRequestStopReportsUnwritableRunDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")
	t.Cleanup(func() { _ = rec.Finish(nil) })
	if err := os.Chmod(rec.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(rec.dir, dirPerm) })
	if _, err := l.RequestStop(rec.ID()); err == nil || errors.Is(err, ErrNotRunning) {
		t.Fatalf("RequestStop in a read-only run directory = %v", err)
	}
}

func TestStopRequestedReportsUnreadableRunDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")
	t.Cleanup(func() { _ = rec.Finish(nil) })
	if err := os.Chmod(rec.dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(rec.dir, dirPerm) })
	if _, err := rec.StopRequested(); err == nil {
		t.Fatal("StopRequested succeeded in an unreadable run directory")
	}
}

func TestStoppedRunsRecordTheirExitStatus(t *testing.T) {
	code := func(n int) *int { return &n }
	for _, tt := range []struct {
		name     string
		runErr   func(t *testing.T) error
		exitCode *int
	}{
		{"exited cleanly", func(*testing.T) error { return nil }, code(0)},
		{"exit status", func(t *testing.T) error { return commandError(t, "exit 3") }, code(3)},
		{"signal", func(t *testing.T) error { return commandError(t, "kill -9 $$") }, nil},
		{"other failure", func(*testing.T) error { return errors.New("wait failed") }, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := New(t.TempDir())
			rec := startRun(t, l, "/srv/a", "one")
			rec.Stopped()
			if err := rec.Finish(tt.runErr(t)); err != nil {
				t.Fatal(err)
			}
			run := readMeta(t, l, rec.ID())
			if run.Status != StatusStopped || run.Error != "stopped on request" {
				t.Fatalf("run = %+v", run)
			}
			if (run.ExitCode == nil) != (tt.exitCode == nil) || (run.ExitCode != nil && *run.ExitCode != *tt.exitCode) {
				t.Fatalf("exit code = %v, want %v", run.ExitCode, tt.exitCode)
			}
		})
	}
}

func TestWaitReturnsTheFinalRecord(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")
	running := readMeta(t, l, rec.ID())

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := l.Wait(ctx, running, 5*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait on a live run = %v", err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = rec.Finish(nil)
	}()
	final, err := l.Wait(t.Context(), running, 5*time.Millisecond)
	if err != nil || final.Status != StatusSucceeded {
		t.Fatalf("Wait = %+v, %v", final, err)
	}
}

func TestWaitReportsLockFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")
	t.Cleanup(func() { _ = rec.Finish(nil) })
	lock := filepath.Join(rec.dir, lockFile)
	if err := os.Chmod(lock, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Wait(t.Context(), rec.run, time.Millisecond); err == nil {
		t.Fatal("Wait succeeded with an unreadable run lock")
	}
}

func TestLiveListsRunningRunsOfOneTask(t *testing.T) {
	l := New(t.TempDir())
	done := startRun(t, l, "/srv/a", "one")
	if err := done.Finish(nil); err != nil {
		t.Fatal(err)
	}
	live := startRun(t, l, "/srv/a", "one")
	t.Cleanup(func() { _ = live.Finish(nil) })
	other := startRun(t, l, "/srv/a", "two")
	t.Cleanup(func() { _ = other.Finish(nil) })

	runs, err := l.Live(TaskID("/srv/a", "one"))
	if err != nil || len(runs) != 1 || runs[0].ID != live.ID() {
		t.Fatalf("Live = %+v, %v", runs, err)
	}
	if runs, err := l.Live(TaskID("/srv/a", "missing")); err != nil || len(runs) != 0 {
		t.Fatalf("Live of an unknown task = %+v, %v", runs, err)
	}

	if err := os.WriteFile(filepath.Join(l.root, TaskID("/srv/a", "one"), live.ID(), metaFile), []byte("{"), outputPerm); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Live(TaskID("/srv/a", "one")); err == nil {
		t.Fatal("Live succeeded with a corrupt run record")
	}
}
