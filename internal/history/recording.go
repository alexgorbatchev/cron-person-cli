package history

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/filelock"
)

const (
	stdoutPrefix = "[out] "
	stderrPrefix = "[err] "
	// maxPartialLine bounds buffering for output that never emits a newline.
	maxPartialLine = 64 * 1024
	outputPerm     = 0o600
	// truncationNotice ends a log whose output exceeded the per-run limit.
	truncationNotice = "[cron-person] output truncated after %d bytes\n"
)

// Recording captures one in-progress run. Its writers never fail: capture
// errors are retained and reported by Finish or Reject, so a full disk cannot
// break the task's own output streams. It holds the run's lock until the
// outcome is recorded, which is how readers tell a live run from one whose
// process died.
type Recording struct {
	run    Run
	dir    string
	mu     sync.Mutex
	lock   *os.File
	output *os.File
	err    error
	// maxOutput and written enforce StartOptions.MaxOutputBytes.
	maxOutput int64
	written   int64
	stdout    *streamWriter
	stderr    *streamWriter
}

// StartOptions describes a run being recorded.
type StartOptions struct {
	Dir     string
	Command string
	Trigger Trigger
	// MaxOutputBytes caps the stored output; zero stores everything.
	MaxOutputBytes int64
	Warnings       []string
}

// Start records a new running task and opens its output log. The run's lock
// is taken before anything else is written, and the metadata last, so a run
// is never visible to readers without its lock held; see Prune for the instant
// before the lock exists.
func (l *Log) Start(opts StartOptions) (*Recording, error) {
	now := time.Now().UTC()
	id, err := newRunID(now)
	if err != nil {
		return nil, err
	}
	dir, command := opts.Dir, opts.Command
	run := Run{
		ID:        id,
		TaskID:    TaskID(dir, command),
		Dir:       dir,
		Command:   command,
		Trigger:   opts.Trigger,
		Status:    StatusRunning,
		StartedAt: now,
		Warnings:  opts.Warnings,
	}

	taskDir := filepath.Join(l.root, run.TaskID)
	if err := os.MkdirAll(taskDir, dirPerm); err != nil {
		return nil, fmt.Errorf("creating run directory: %w", err)
	}
	if err := writeJSON(filepath.Join(taskDir, taskFile), taskInfo{Dir: dir, Command: command}); err != nil {
		return nil, err
	}
	rec := &Recording{run: run, dir: filepath.Join(taskDir, id), maxOutput: opts.MaxOutputBytes}
	rec.stdout = &streamWriter{rec: rec, prefix: stdoutPrefix}
	rec.stderr = &streamWriter{rec: rec, prefix: stderrPrefix}
	if err := os.Mkdir(rec.dir, dirPerm); err != nil {
		return nil, fmt.Errorf("creating run directory: %w", err)
	}
	if err := rec.open(); err != nil {
		return nil, errors.Join(err, rec.closeFiles(), os.RemoveAll(rec.dir))
	}
	return rec, nil
}

func (r *Recording) open() error {
	var err error
	if r.lock, err = os.OpenFile(filepath.Join(r.dir, lockFile), os.O_CREATE|os.O_EXCL|os.O_RDWR, outputPerm); err != nil {
		return fmt.Errorf("creating run lock: %w", err)
	}
	if err := filelock.TryLock(r.lock); err != nil {
		return fmt.Errorf("locking run: %w", err)
	}
	if r.output, err = os.OpenFile(filepath.Join(r.dir, outputFile), os.O_CREATE|os.O_EXCL|os.O_WRONLY, outputPerm); err != nil {
		return fmt.Errorf("creating run output: %w", err)
	}
	return writeJSON(filepath.Join(r.dir, metaFile), r.run)
}

// closeFiles releases the output and the lock, which must come last so the
// run reads as live until its outcome is on disk.
func (r *Recording) closeFiles() error {
	var errs []error
	if r.output != nil {
		errs = append(errs, r.output.Close())
	}
	if r.lock != nil {
		errs = append(errs, r.lock.Close())
	}
	return errors.Join(errs...)
}

// ID returns the run identifier.
func (r *Recording) ID() string { return r.run.ID }

// Stdout returns a writer capturing the command's standard output.
func (r *Recording) Stdout() io.Writer { return r.stdout }

// Stderr returns a writer capturing the command's standard error.
func (r *Recording) Stderr() io.Writer { return r.stderr }

// Finish records the command's outcome. runErr is the error returned by
// running the command, or nil on success.
func (r *Recording) Finish(runErr error) error {
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		code := 0
		r.run.Status, r.run.ExitCode = StatusSucceeded, &code
	case errors.As(runErr, &exitErr):
		r.run.Status, r.run.Error = StatusFailed, runErr.Error()
		// ExitCode reports -1 for signal termination, which has no exit status.
		if code := exitErr.ExitCode(); code >= 0 {
			r.run.ExitCode = &code
		}
	default:
		r.run.Status, r.run.Error = StatusFailed, runErr.Error()
	}
	return r.close()
}

// Warn records a problem that does not change the run's outcome.
func (r *Recording) Warn(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.run.Warnings = append(r.run.Warnings, msg)
}

// Reject records that the command was refused before it started.
func (r *Recording) Reject(reason error) error {
	r.run.Status, r.run.Error = StatusBlocked, reason.Error()
	return r.close()
}

func (r *Recording) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stdout.flushLocked()
	r.stderr.flushLocked()
	r.run.FinishedAt = time.Now().UTC()
	metaErr := writeJSON(filepath.Join(r.dir, metaFile), r.run)
	closeErr := r.closeFiles()
	if err := errors.Join(r.err, metaErr, closeErr); err != nil {
		return fmt.Errorf("recording run %s: %w", r.run.ID, err)
	}
	return nil
}

// writeLineLocked writes line, which excludes its newline, as one or more
// prefixed records no longer than maxPartialLine.
func (r *Recording) writeLineLocked(prefix string, line []byte) {
	for {
		chunk := line[:min(len(line), maxPartialLine)]
		r.writeRecordLocked(prefix, chunk)
		line = line[len(chunk):]
		if len(line) == 0 {
			return
		}
	}
}

// writeRecordLocked stores one prefixed record, or the truncation notice once
// the record would exceed the output limit.
func (r *Recording) writeRecordLocked(prefix string, chunk []byte) {
	if r.err != nil || r.run.Truncated {
		return
	}
	size := int64(len(prefix) + len(chunk) + 1)
	if r.maxOutput > 0 && r.written+size > r.maxOutput {
		r.run.Truncated = true
		_, err := fmt.Fprintf(r.output, truncationNotice, r.maxOutput)
		r.setWriteErr(err)
		return
	}
	n, err := fmt.Fprintf(r.output, "%s%s\n", prefix, chunk)
	r.written += int64(n)
	r.setWriteErr(err)
}

func (r *Recording) setWriteErr(err error) {
	if err != nil {
		r.err = fmt.Errorf("writing run output: %w", err)
	}
}

// streamWriter prefixes complete lines so interleaved stdout and stderr stay
// attributable in a single chronological log.
type streamWriter struct {
	rec    *Recording
	prefix string
	buf    []byte
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.rec.mu.Lock()
	defer w.rec.mu.Unlock()
	w.buf = append(w.buf, p...)
	start := 0
	for {
		i := bytes.IndexByte(w.buf[start:], '\n')
		if i < 0 {
			break
		}
		w.rec.writeLineLocked(w.prefix, w.buf[start:start+i])
		start += i + 1
	}
	w.buf = append(w.buf[:0], w.buf[start:]...)
	if len(w.buf) >= maxPartialLine {
		w.flushLocked()
	}
	return len(p), nil
}

func (w *streamWriter) flushLocked() {
	if len(w.buf) == 0 {
		return
	}
	w.rec.writeLineLocked(w.prefix, w.buf)
	w.buf = w.buf[:0]
}
