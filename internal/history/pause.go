package history

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/filelock"
)

// pauseLockFile, in the history root, serializes every change to pause
// records, so removing an expired pause can never delete one that `task
// pause` is writing at the same moment. Reading a pause needs no lock, because
// records are replaced atomically.
const pauseLockFile = "pause.lock"

// ErrNotPaused reports a resume of a task that has no pause.
var ErrNotPaused = errors.New("task is not paused")

// pause is the record that holds a task off its schedule.
type pause struct {
	Until    time.Time `json:"until"`
	PausedAt time.Time `json:"paused_at"`
}

// Pause holds the task identified by dir and command off its schedule until
// until, replacing any earlier pause of the task.
func (l *Log) Pause(dir, command string, until time.Time) (err error) {
	unlock, err := l.lockPauses()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	taskDir := filepath.Join(l.root, TaskID(dir, command))
	if err := os.MkdirAll(taskDir, dirPerm); err != nil {
		return fmt.Errorf("creating task directory: %w", err)
	}
	if err := writeJSON(filepath.Join(taskDir, taskFile), taskInfo{Dir: dir, Command: command}); err != nil {
		return err
	}
	return writeJSON(filepath.Join(taskDir, pauseFile), pause{Until: until.UTC(), PausedAt: time.Now().UTC()})
}

// Resume ends the pause of the task identified by dir and command. A task
// with no pause, or one that has already expired, reports ErrNotPaused; an
// expired pause is removed either way. A pause record that cannot be read is
// removed too, so resuming always clears a broken pause.
func (l *Log) Resume(dir, command string, now time.Time) (err error) {
	unlock, err := l.lockPauses()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	taskID := TaskID(dir, command)
	_, paused, readErr := l.PausedUntil(taskID, now)
	err = os.Remove(filepath.Join(l.root, taskID, pauseFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return ErrNotPaused
	case err != nil:
		return errors.Join(readErr, fmt.Errorf("resuming task: %w", err))
	case readErr == nil && !paused:
		return ErrNotPaused
	}
	return nil
}

// PausedUntil reports when the pause of taskID ends, and whether it is still
// in force at now.
func (l *Log) PausedUntil(taskID string, now time.Time) (time.Time, bool, error) {
	var p pause
	err := readJSON(filepath.Join(l.root, taskID, pauseFile), &p)
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return p.Until, now.Before(p.Until), nil
}

// lockPauses takes the lock that serializes changes to pause records.
func (l *Log) lockPauses() (func() error, error) {
	if err := os.MkdirAll(l.root, dirPerm); err != nil {
		return nil, fmt.Errorf("creating history directory: %w", err)
	}
	unlock, err := filelock.LockPath(filepath.Join(l.root, pauseLockFile))
	if err != nil {
		return nil, fmt.Errorf("locking pauses: %w", err)
	}
	return unlock, nil
}

// pruneExpiredPauses removes pause records that ended before now. A record
// that cannot be read is left for `task resume`, which clears it.
func (l *Log) pruneExpiredPauses(now time.Time) (err error) {
	taskDirs, err := os.ReadDir(l.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading history: %w", err)
	}
	unlock, err := l.lockPauses()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	var errs []error
	for _, td := range taskDirs {
		if !td.IsDir() {
			continue
		}
		var p pause
		path := filepath.Join(l.root, td.Name(), pauseFile)
		if err := readJSON(path, &p); err != nil || now.Before(p.Until) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("removing expired pause: %w", err))
		}
	}
	return errors.Join(errs...)
}
