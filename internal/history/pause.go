package history

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrNotPaused reports a resume of a task that has no pause.
var ErrNotPaused = errors.New("task is not paused")

// pause is the record that holds a task off its schedule.
type pause struct {
	Until    time.Time `json:"until"`
	PausedAt time.Time `json:"paused_at"`
}

// Pause holds the task identified by dir and command off its schedule until
// until, replacing any earlier pause of the task.
func (l *Log) Pause(dir, command string, until time.Time) error {
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
// expired pause is removed either way.
func (l *Log) Resume(dir, command string, now time.Time) error {
	taskID := TaskID(dir, command)
	_, paused, err := l.PausedUntil(taskID, now)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(l.root, taskID, pauseFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("resuming task: %w", err)
	}
	if !paused {
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
