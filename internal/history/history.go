// Package history records task executions and their captured output.
//
// Runs are stored as <root>/<task-id>/<run-id>/ directories holding meta.json,
// output.log, and run.lock. Each task directory also holds task.json
// identifying the directory and command the task ID was derived from. Hidden
// entries are runs still being created and are ignored by readers.
package history

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/filelock"
)

// Trigger identifies what started a run.
type Trigger string

const (
	TriggerCron   Trigger = "cron"
	TriggerManual Trigger = "manual"
)

// Status is the lifecycle state of a run.
type Status string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	// StatusBlocked marks runs refused before the command started, such as
	// unauthorized or modified .cronrc files.
	StatusBlocked Status = "blocked"
	// StatusInterrupted marks runs whose recording process exited without
	// recording an outcome. It is derived on read from the released run lock
	// and never written.
	StatusInterrupted Status = "interrupted"
)

const (
	taskFile    = "task.json"
	metaFile    = "meta.json"
	outputFile  = "output.log"
	lockFile    = "run.lock"
	dirPerm     = 0o700
	runIDLayout = "20060102T150405.000000Z"
	taskIDBytes = 6
)

// ErrNotFound is returned when a run ID does not exist in the log.
var ErrNotFound = errors.New("run not found")

// Run describes a single task execution.
type Run struct {
	ID         string    `json:"id"`
	TaskID     string    `json:"task_id"`
	Dir        string    `json:"dir"`
	Command    string    `json:"command"`
	Trigger    Trigger   `json:"trigger"`
	Status     Status    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	// ExitCode is nil when the command never produced an exit status, such as
	// blocked runs, runs still in progress, or commands terminated by a signal.
	ExitCode *int   `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
	// Warnings are problems that did not stop the run, such as an unreadable
	// configuration replaced by defaults.
	Warnings []string `json:"warnings,omitempty"`
	// Truncated reports that output beyond the per-run limit was not stored.
	Truncated bool `json:"truncated,omitempty"`
}

// Duration returns the run's elapsed time, or zero while it is unfinished.
func (r Run) Duration() time.Duration {
	if r.FinishedAt.IsZero() {
		return 0
	}
	return r.FinishedAt.Sub(r.StartedAt)
}

type taskInfo struct {
	Dir     string `json:"dir"`
	Command string `json:"command"`
}

// TaskID derives the stable identifier for a command in a directory. Editing
// the command produces a new task with its own history.
func TaskID(dir, command string) string {
	sum := sha256.Sum256([]byte(dir + "\x00" + command))
	return hex.EncodeToString(sum[:taskIDBytes])
}

// DirForStore returns the history directory kept beside an authorization store,
// so custom --store locations keep separate histories.
func DirForStore(storePath string) string {
	return filepath.Join(filepath.Dir(storePath), "runs")
}

// Log reads and writes run records under a root directory.
type Log struct {
	root string
}

// New returns a Log rooted at dir.
func New(dir string) *Log {
	return &Log{root: dir}
}

// Filter narrows List results. Zero fields match everything.
type Filter struct {
	Dir    string
	TaskID string
	Limit  int
}

type runRef struct {
	taskID, runID string
}

// scan returns references to every published run in task directories accepted
// by keep, oldest first. Entries not named like run IDs, including hidden
// staging directories, are ignored. Run IDs start with a fixed-width UTC timestamp, so name
// order is time order.
func (l *Log) scan(keep func(taskID string) (bool, error)) ([]runRef, error) {
	taskDirs, err := os.ReadDir(l.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading history: %w", err)
	}

	var refs []runRef
	for _, td := range taskDirs {
		if !td.IsDir() || hidden(td.Name()) {
			continue
		}
		if keep != nil {
			ok, err := keep(td.Name())
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		runDirs, err := os.ReadDir(filepath.Join(l.root, td.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading task history %s: %w", td.Name(), err)
		}
		for _, rd := range runDirs {
			if _, ok := runStarted(rd.Name()); rd.IsDir() && ok {
				refs = append(refs, runRef{taskID: td.Name(), runID: rd.Name()})
			}
		}
	}
	slices.SortFunc(refs, func(a, b runRef) int { return strings.Compare(a.runID, b.runID) })
	return refs, nil
}

// List returns matching runs, newest first.
func (l *Log) List(f Filter) ([]Run, error) {
	refs, err := l.scan(func(taskID string) (bool, error) {
		if f.TaskID != "" && taskID != f.TaskID {
			return false, nil
		}
		if f.Dir == "" {
			return true, nil
		}
		var info taskInfo
		if err := readJSON(filepath.Join(l.root, taskID, taskFile), &info); err != nil {
			return false, err
		}
		return info.Dir == f.Dir, nil
	})
	if err != nil {
		return nil, err
	}
	slices.Reverse(refs)
	if f.Limit > 0 && len(refs) > f.Limit {
		refs = refs[:f.Limit]
	}

	runs := make([]Run, 0, len(refs))
	for _, ref := range refs {
		run, err := l.readRun(ref)
		if errors.Is(err, os.ErrNotExist) {
			// Pruned by a concurrent run since the scan.
			continue
		}
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// Get returns the run with the given ID.
func (l *Log) Get(id string) (Run, error) {
	if id == "" || hidden(id) || filepath.Base(id) != id {
		return Run{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	taskDirs, err := os.ReadDir(l.root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Run{}, fmt.Errorf("reading history: %w", err)
	}
	for _, td := range taskDirs {
		if !td.IsDir() || hidden(td.Name()) {
			continue
		}
		ref := runRef{taskID: td.Name(), runID: id}
		run, err := l.readRun(ref)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		return run, err
	}
	return Run{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Retention bounds stored history. A zero field disables that bound.
type Retention struct {
	MaxAge   time.Duration
	MaxBytes int64
}

type pruneCandidate struct {
	ref  runRef
	size int64
}

// Prune deletes finished runs older than MaxAge, then the oldest finished runs
// until history fits within MaxBytes. Runs in progress and the newest run of
// every task are always kept, so MaxBytes is a target that a single large run
// can exceed.
func (l *Log) Prune(r Retention, now time.Time) error {
	if r.MaxAge <= 0 && r.MaxBytes <= 0 {
		return nil
	}
	refs, err := l.scan(nil)
	if err != nil {
		return err
	}
	newest := make(map[string]string, len(refs))
	for _, ref := range refs {
		newest[ref.taskID] = ref.runID
	}

	var total int64
	var candidates []pruneCandidate
	for _, ref := range refs {
		size, err := dirSize(l.runDir(ref))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		total += size
		if newest[ref.taskID] == ref.runID {
			continue
		}
		live, err := l.locked(ref)
		if err != nil {
			return err
		}
		if !live {
			candidates = append(candidates, pruneCandidate{ref: ref, size: size})
		}
	}

	var errs []error
	remove := func(c pruneCandidate) {
		if err := os.RemoveAll(l.runDir(c.ref)); err != nil {
			errs = append(errs, fmt.Errorf("pruning run %s: %w", c.ref.runID, err))
			return
		}
		total -= c.size
	}
	kept := candidates[:0]
	for _, c := range candidates {
		if started, _ := runStarted(c.ref.runID); r.MaxAge > 0 && now.Sub(started) > r.MaxAge {
			remove(c)
			continue
		}
		kept = append(kept, c)
	}
	for _, c := range kept {
		if r.MaxBytes <= 0 || total <= r.MaxBytes {
			break
		}
		remove(c)
	}
	return errors.Join(errs...)
}

func dirSize(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("measuring run %s: %w", dir, err)
		}
		total += info.Size()
	}
	return total, nil
}

// runStarted parses the start time encoded in a run ID.
func runStarted(id string) (time.Time, bool) {
	prefix, _, ok := strings.Cut(id, "-")
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(runIDLayout, prefix)
	return t, err == nil
}

func hidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

// WriteOutput copies a run's captured output to w. A positive tail limits the
// copy to the last tail lines.
func (l *Log) WriteOutput(w io.Writer, run Run, tail int) error {
	f, err := os.Open(l.outputPath(run.TaskID, run.ID))
	if err != nil {
		return fmt.Errorf("opening run output: %w", err)
	}
	defer f.Close()
	return writeTail(w, f, tail)
}

// Follow writes a run's captured output to w, then keeps writing output as it
// is appended until the run's recorder finishes, polling every interval. It
// returns the run as finally recorded. A positive tail limits the output
// already present to its last tail lines.
func (l *Log) Follow(ctx context.Context, w io.Writer, run Run, tail int, interval time.Duration) (Run, error) {
	ref := runRef{taskID: run.TaskID, runID: run.ID}
	f, err := os.Open(l.outputPath(run.TaskID, run.ID))
	if err != nil {
		return Run{}, fmt.Errorf("opening run output: %w", err)
	}
	defer f.Close()
	if err := writeTail(w, f, tail); err != nil {
		return Run{}, err
	}
	for {
		// Probe before copying: output is complete once the lock is released, so
		// a copy that follows a released probe cannot miss the final lines.
		live, err := l.locked(ref)
		if err != nil {
			return Run{}, err
		}
		if _, err := io.Copy(w, f); err != nil {
			return Run{}, fmt.Errorf("reading run output: %w", err)
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

// writeTail copies r to w, or only its last tail lines when tail is positive,
// leaving r positioned at the end of what it read.
func writeTail(w io.Writer, r io.Reader, tail int) error {
	if tail <= 0 {
		if _, err := io.Copy(w, r); err != nil {
			return fmt.Errorf("reading run output: %w", err)
		}
		return nil
	}

	lines := make([]string, 0, tail)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxPartialLine+len(stderrPrefix)+2)
	for sc.Scan() {
		if len(lines) == tail {
			lines = lines[1:]
		}
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("reading run output: %w", err)
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) readRun(ref runRef) (Run, error) {
	run, err := l.readMeta(ref)
	if err != nil || run.Status != StatusRunning {
		return run, err
	}
	live, err := l.locked(ref)
	if err != nil || live {
		return run, err
	}
	// The recorder may have stored its outcome between the two reads.
	if run, err = l.readMeta(ref); err != nil || run.Status != StatusRunning {
		return run, err
	}
	run.Status = StatusInterrupted
	return run, nil
}

func (l *Log) readMeta(ref runRef) (Run, error) {
	var run Run
	err := readJSON(filepath.Join(l.runDir(ref), metaFile), &run)
	return run, err
}

// locked reports whether a live recorder still holds the run's lock. The
// operating system drops the lock when its holder exits, however it exits.
func (l *Log) locked(ref runRef) (bool, error) {
	f, err := os.OpenFile(filepath.Join(l.runDir(ref), lockFile), os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("opening run lock: %w", err)
	}
	defer f.Close()
	err = filelock.TryLock(f)
	if errors.Is(err, filelock.ErrLocked) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("probing run lock: %w", err)
	}
	return false, nil
}

func (l *Log) runDir(ref runRef) string {
	return filepath.Join(l.root, ref.taskID, ref.runID)
}

func (l *Log) outputPath(taskID, runID string) string {
	return filepath.Join(l.root, taskID, runID, outputFile)
}

func newRunID(t time.Time) (string, error) {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("generating run id: %w", err)
	}
	return t.UTC().Format(runIDLayout) + "-" + hex.EncodeToString(suffix), nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}

// writeJSON replaces path atomically so readers never observe partial records.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("committing %s: %w", path, err)
	}
	return nil
}
