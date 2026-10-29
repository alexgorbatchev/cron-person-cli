package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/history"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

// ExecOptions defines the parameters for executing a command in a directory.
type ExecOptions struct {
	Dir      string
	Command  string
	Stdout   io.Writer
	Stderr   io.Writer
	Stdin    io.Reader
	ExtraEnv map[string]string
	// History records the run, including refusals, when set. Retention is
	// enforced after each recorded run.
	History   *history.Log
	Trigger   history.Trigger
	Retention history.Retention
	// MaxOutputBytes caps each run's stored output; zero stores everything.
	MaxOutputBytes int64
	// Warnings are recorded with the run.
	Warnings []string
	// OnStart receives the run ID once recording has started.
	OnStart func(runID string)
	// Grace bounds two waits: how long a cancelled task may take to exit after
	// SIGTERM before its process group is killed, and how long output is still
	// captured after the task exits while a background process it started keeps
	// the output streams open. Zero selects defaultGrace.
	Grace time.Duration
}

const defaultGrace = 10 * time.Second

// Runner executes tasks within the context of an authorized directory and its .cronrc.
type Runner struct {
	store *store.Store
}

// NewRunner creates a new Runner backed by the authorization store.
func NewRunner(st *store.Store) *Runner {
	return &Runner{
		store: st,
	}
}

// Exec verifies directory authorization, sets working directory to the .cronrc location,
// loads .cronrc environment variables, and executes the specified shell command.
// History failures never prevent the task from running; they are joined into the
// returned error after the task completes.
func (r *Runner) Exec(ctx context.Context, opts ExecOptions) error {
	absDir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return fmt.Errorf("resolving target directory %q: %w", opts.Dir, err)
	}

	var rec *history.Recording
	var historyErr error
	if opts.History != nil {
		start := history.StartOptions{Dir: absDir, Command: opts.Command, Trigger: opts.Trigger, MaxOutputBytes: opts.MaxOutputBytes, Warnings: opts.Warnings}
		if rec, err = opts.History.Start(start); err != nil {
			historyErr = fmt.Errorf("recording run: %w", err)
		} else if opts.OnStart != nil {
			opts.OnStart(rec.ID())
		}
	}

	cmd, err := r.prepare(ctx, absDir, opts)
	if err != nil {
		if rec != nil {
			historyErr = rec.Reject(err)
		}
		return errors.Join(err, historyErr, prune(opts))
	}

	cmd.Stdout = defaultWriter(opts.Stdout, os.Stdout)
	cmd.Stderr = defaultWriter(opts.Stderr, os.Stderr)
	if rec != nil {
		// Distinct tee writers defeat os/exec's single-writer guarantee for a
		// shared stdout/stderr writer, so caller writes share one lock. Caller
		// streams are best effort: once one fails, such as a detached `task run`
		// closing its end, output keeps flowing into the recording, which never
		// fails, and the task is never blocked or broken by it.
		mu := &sync.Mutex{}
		cmd.Stdout = io.MultiWriter(rec.Stdout(), &passthroughWriter{mu: mu, w: cmd.Stdout})
		cmd.Stderr = io.MultiWriter(rec.Stderr(), &passthroughWriter{mu: mu, w: cmd.Stderr})
	}
	cmd.Stdin = opts.Stdin
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}

	grace := opts.Grace
	if grace <= 0 {
		grace = defaultGrace
	}
	runErr := runGroup(cmd, grace)
	if errors.Is(runErr, exec.ErrWaitDelay) {
		if rec != nil {
			rec.Warn(fmt.Sprintf("output stayed open %s after the task exited, so capture stopped; redirect the output of background processes", grace))
		}
		runErr = nil
	}
	if runErr != nil {
		runErr = fmt.Errorf("task failed in %s: %w", absDir, runErr)
	}
	if rec != nil {
		historyErr = rec.Finish(runErr)
	}
	return errors.Join(runErr, historyErr, prune(opts))
}

// runGroup runs cmd so that cancellation reaches every process the task
// started: a graceful stop first (SIGTERM, or Ctrl+Break on Windows), then a
// forced kill of the whole group once grace passes.
func runGroup(cmd *exec.Cmd, grace time.Duration) error {
	prepareGroup(cmd)
	var group atomic.Pointer[taskGroup]
	var escalation atomic.Pointer[time.Timer]
	cmd.Cancel = func() error {
		g := group.Load()
		if g == nil {
			return cmd.Process.Kill()
		}
		escalation.Store(time.AfterFunc(grace, func() { _ = g.kill() }))
		return g.terminate()
	}
	cmd.WaitDelay = grace
	if err := cmd.Start(); err != nil {
		return err
	}
	g, err := attachGroup(cmd)
	if err != nil {
		return errors.Join(fmt.Errorf("tracking task processes: %w", err), cmd.Process.Kill(), cmd.Wait())
	}
	group.Store(g)
	err = cmd.Wait()
	if t := escalation.Load(); t != nil {
		t.Stop()
	}
	return errors.Join(err, g.close())
}

func prune(opts ExecOptions) error {
	if opts.History == nil {
		return nil
	}
	if err := opts.History.Prune(opts.Retention, time.Now()); err != nil {
		return fmt.Errorf("pruning run history: %w", err)
	}
	return nil
}

// passthroughWriter forwards to a caller stream until its first failure and
// then discards, always reporting success so the tee keeps recording.
type passthroughWriter struct {
	mu     *sync.Mutex
	w      io.Writer
	broken bool
}

func (w *passthroughWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.broken {
		if _, err := w.w.Write(p); err != nil {
			w.broken = true
		}
	}
	return len(p), nil
}

func defaultWriter(w, fallback io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return fallback
}

// prepare validates authorization and builds the command from the validated
// .cronrc snapshot.
func (r *Runner) prepare(ctx context.Context, absDir string, opts ExecOptions) (*exec.Cmd, error) {
	// 1. Verify authorization status and read the validated snapshot
	dirStatus, _, snapshot, err := r.store.Inspect(absDir)
	if err != nil {
		return nil, fmt.Errorf("checking authorization for %s: %w", absDir, err)
	}

	switch dirStatus {
	case store.StatusAllowed:
		// Authorization valid
	case store.StatusUnauthorized:
		return nil, fmt.Errorf("directory %s is unauthorized: run 'cron-person allow %s' to authorize", absDir, absDir)
	case store.StatusChanged:
		return nil, fmt.Errorf("directory %s has changed .cronrc (hash mismatch / blocked): run 'cron-person allow %s' to approve changes", absDir, absDir)
	case store.StatusMissingCronrc:
		return nil, fmt.Errorf("no .cronrc file found in %s", absDir)
	case store.StatusOrphan:
		return nil, fmt.Errorf("directory %s or its .cronrc does not exist on disk", absDir)
	default:
		return nil, fmt.Errorf("directory %s has invalid status: %s", absDir, dirStatus)
	}

	// 2. Parse the validated .cronrc snapshot to obtain directory-specific environment variables
	cronrcPath := filepath.Join(absDir, ".cronrc")
	parsed, err := cronrc.Parse(bytes.NewReader(snapshot), cronrcPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", cronrcPath, err)
	}

	// 3. Prepare command execution environment
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	if parsedShell, ok := parsed.Env["SHELL"]; ok && parsedShell != "" {
		shell = parsedShell
	}

	cmd := exec.CommandContext(ctx, shell, "-c", opts.Command)
	cmd.Dir = absDir

	// Build environment: current process env + .cronrc env + extra env
	envMap := make(map[string]string)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			envMap[k] = v
		}
	}
	for k, v := range parsed.Env {
		envMap[k] = v
	}
	for k, v := range opts.ExtraEnv {
		envMap[k] = v
	}

	// Injected CRON_PERSON helper environment variables
	envMap["CRON_PERSON_DIR"] = absDir
	envMap["CRON_PERSON_CRONRC"] = cronrcPath

	envList := make([]string, 0, len(envMap))
	for k, v := range envMap {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = envList

	return cmd, nil
}
