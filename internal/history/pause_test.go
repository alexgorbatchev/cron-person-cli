package history

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPauseHoldsATaskUntilItEnds(t *testing.T) {
	l := New(t.TempDir())
	now := time.Now()
	id := TaskID("/srv/a", "one")

	if _, paused, err := l.PausedUntil(id, now); err != nil || paused {
		t.Fatalf("before pause: %v, %v", paused, err)
	}
	until := now.Add(time.Hour)
	if err := l.Pause("/srv/a", "one", until); err != nil {
		t.Fatal(err)
	}
	got, paused, err := l.PausedUntil(id, now)
	if err != nil || !paused || !got.Equal(until.UTC().Round(0)) {
		t.Fatalf("PausedUntil = %v, %v, %v", got, paused, err)
	}
	if _, paused, err := l.PausedUntil(id, until.Add(time.Second)); err != nil || paused {
		t.Fatalf("after the end: %v, %v", paused, err)
	}

	// Pausing again replaces the end time, and the pause names its task.
	later := now.Add(2 * time.Hour)
	if err := l.Pause("/srv/a", "one", later); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := l.PausedUntil(id, now); !got.Equal(later.UTC().Round(0)) {
		t.Fatalf("re-paused until %v, want %v", got, later)
	}
	var info taskInfo
	if err := readJSON(filepath.Join(l.root, id, taskFile), &info); err != nil || info.Dir != "/srv/a" || info.Command != "one" {
		t.Fatalf("task info = %+v, %v", info, err)
	}

	// A pause is not a run: history and pruning ignore it.
	if runs, err := l.List(Filter{}); err != nil || len(runs) != 0 {
		t.Fatalf("List = %+v, %v", runs, err)
	}
	if err := l.Prune(Retention{MaxAge: time.Nanosecond}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, paused, _ := l.PausedUntil(id, now); !paused {
		t.Fatal("Prune removed the pause")
	}

	if err := l.Resume("/srv/a", "one", now); err != nil {
		t.Fatal(err)
	}
	if _, paused, err := l.PausedUntil(id, now); err != nil || paused {
		t.Fatalf("after resume: %v, %v", paused, err)
	}
	if err := l.Resume("/srv/a", "one", now); !errors.Is(err, ErrNotPaused) {
		t.Fatalf("second resume = %v", err)
	}
}

func TestResumeRemovesAnExpiredPause(t *testing.T) {
	l := New(t.TempDir())
	now := time.Now()
	if err := l.Pause("/srv/a", "one", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := l.Resume("/srv/a", "one", now); !errors.Is(err, ErrNotPaused) {
		t.Fatalf("Resume of an expired pause = %v", err)
	}
	if _, err := os.Stat(filepath.Join(l.root, TaskID("/srv/a", "one"), pauseFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired pause left behind: %v", err)
	}
}

func TestPauseReportsStorageFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	root := t.TempDir()
	l := New(root)
	id := TaskID("/srv/a", "one")

	blocked := New(filepath.Join(root, "file"))
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := blocked.Pause("/srv/a", "one", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("Pause succeeded under a file")
	}

	if err := os.MkdirAll(filepath.Join(root, id, taskFile), dirPerm); err != nil {
		t.Fatal(err)
	}
	if err := l.Pause("/srv/a", "one", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("Pause succeeded with an unwritable task record")
	}
	if err := os.Remove(filepath.Join(root, id, taskFile)); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, id, pauseFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.PausedUntil(id, time.Now()); err == nil {
		t.Fatal("PausedUntil accepted a corrupt pause")
	}
	if err := l.Resume("/srv/a", "one", time.Now()); err == nil || errors.Is(err, ErrNotPaused) {
		t.Fatalf("Resume of a corrupt pause = %v", err)
	}

	if err := l.Pause("/srv/a", "one", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, id), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, id), dirPerm) })
	if err := l.Resume("/srv/a", "one", time.Now()); err == nil || errors.Is(err, ErrNotPaused) {
		t.Fatalf("Resume in a read-only task directory = %v", err)
	}
}

func TestSkipRecordsAPausedRun(t *testing.T) {
	l := New(t.TempDir())
	rec := startRun(t, l, "/srv/a", "one")
	if err := rec.Skip("task is paused until later"); err != nil {
		t.Fatal(err)
	}
	run := readMeta(t, l, rec.ID())
	if run.Status != StatusPaused || run.Error != "task is paused until later" || run.ExitCode != nil {
		t.Fatalf("run = %+v", run)
	}
}
