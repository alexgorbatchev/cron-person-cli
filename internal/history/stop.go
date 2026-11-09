package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RequestStop asks the recorder of the live run id to stop its task, and
// returns the run as it was when asked. The recorder notices the request
// within its polling interval and ends the task through its graceful
// termination path, so the outcome is still recorded. A run that has already
// ended reports ErrNotRunning.
func (l *Log) RequestStop(id string) (Run, error) {
	run, err := l.Get(id)
	if err != nil {
		return Run{}, err
	}
	if run.Status != StatusRunning {
		return run, fmt.Errorf("%w: %s is %s", ErrNotRunning, id, run.Status)
	}
	path := filepath.Join(l.runDir(runRef{taskID: run.TaskID, runID: run.ID}), stopFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, outputPerm)
	if err != nil {
		return run, fmt.Errorf("requesting stop of %s: %w", id, err)
	}
	if err := f.Close(); err != nil {
		return run, fmt.Errorf("requesting stop of %s: %w", id, err)
	}
	return run, nil
}

// Wait polls run until its recorder releases the run lock, then returns its
// final record. It returns the context's error when ctx ends first.
func (l *Log) Wait(ctx context.Context, run Run, interval time.Duration) (Run, error) {
	ref := runRef{taskID: run.TaskID, runID: run.ID}
	for {
		live, err := l.locked(ref)
		if err != nil {
			return Run{}, err
		}
		if !live {
			return l.readRun(ref)
		}
		select {
		case <-ctx.Done():
			return Run{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Live returns the runs of taskID that are still running, newest first.
func (l *Log) Live(taskID string) ([]Run, error) {
	runs, err := l.List(Filter{TaskID: taskID})
	if err != nil {
		return nil, err
	}
	live := runs[:0]
	for _, run := range runs {
		if run.Status == StatusRunning {
			live = append(live, run)
		}
	}
	return live, nil
}

// StopRequested reports whether a stop of this run has been requested.
func (r *Recording) StopRequested() (bool, error) {
	_, err := os.Stat(filepath.Join(r.dir, stopFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking stop request: %w", err)
	}
	return true, nil
}

// Stopped records that the task ended because a stop was requested. Finish
// then records StatusStopped whatever the task's exit status.
func (r *Recording) Stopped() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
}
