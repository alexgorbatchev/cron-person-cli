package filelock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openLock(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestTryLockReportsHeldLocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	holder, probe := openLock(t, path), openLock(t, path)

	if err := Lock(holder); err != nil {
		t.Fatal(err)
	}
	if err := TryLock(probe); !errors.Is(err, ErrLocked) {
		t.Fatalf("TryLock while held = %v, want ErrLocked", err)
	}
	if err := Unlock(holder); err != nil {
		t.Fatal(err)
	}
	if err := TryLock(probe); err != nil {
		t.Fatalf("TryLock after release = %v", err)
	}
	if err := TryLock(holder); !errors.Is(err, ErrLocked) {
		t.Fatalf("TryLock against new holder = %v, want ErrLocked", err)
	}
}

func TestClosingReleasesLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	holder := openLock(t, path)
	if err := TryLock(holder); err != nil {
		t.Fatal(err)
	}
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := TryLock(openLock(t, path)); err != nil {
		t.Fatalf("lock survived its holder closing: %v", err)
	}
}

func TestLockErrorsOnClosedFile(t *testing.T) {
	f := openLock(t, filepath.Join(t.TempDir(), "run.lock"))
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	for name, fn := range map[string]func(*os.File) error{"Lock": Lock, "TryLock": TryLock, "Unlock": Unlock} {
		if err := fn(f); err == nil || errors.Is(err, ErrLocked) {
			t.Fatalf("%s on closed file = %v, want an OS error", name, err)
		}
	}
}

func TestLockPathWaitsForTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.lock")
	holder, err := LockPath(path)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan *os.File, 1)
	go func() {
		f, err := LockPath(path)
		if err != nil {
			t.Error(err)
		}
		acquired <- f
	}()
	select {
	case <-acquired:
		t.Fatal("second LockPath acquired a held lock")
	case <-time.After(100 * time.Millisecond):
	}
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-acquired:
		_ = f.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("LockPath never acquired the released lock")
	}
	if _, err := LockPath(filepath.Join(path, "missing", "update.lock")); err == nil {
		t.Fatal("LockPath succeeded in a missing directory")
	}
}
