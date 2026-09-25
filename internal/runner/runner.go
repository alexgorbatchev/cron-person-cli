package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
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
}

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
func (r *Runner) Exec(ctx context.Context, opts ExecOptions) error {
	absDir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return fmt.Errorf("resolving target directory %q: %w", opts.Dir, err)
	}

	// 1. Verify authorization status
	dirStatus, _, err := r.store.Status(absDir)
	if err != nil {
		return fmt.Errorf("checking authorization for %s: %w", absDir, err)
	}

	switch dirStatus {
	case store.StatusAllowed:
		// Authorization valid
	case store.StatusUnauthorized:
		return fmt.Errorf("directory %s is unauthorized: run 'cron-person allow %s' to authorize", absDir, absDir)
	case store.StatusChanged:
		return fmt.Errorf("directory %s has changed .cronrc (hash mismatch / blocked): run 'cron-person allow %s' to approve changes", absDir, absDir)
	case store.StatusMissingCronrc:
		return fmt.Errorf("no .cronrc file found in %s", absDir)
	case store.StatusOrphan:
		return fmt.Errorf("directory %s or its .cronrc does not exist on disk", absDir)
	default:
		return fmt.Errorf("directory %s has invalid status: %s", absDir, dirStatus)
	}

	// 2. Parse .cronrc to obtain directory-specific environment variables
	cronrcPath := filepath.Join(absDir, ".cronrc")
	parsed, err := cronrc.ParseFile(cronrcPath)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", cronrcPath, err)
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
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				envMap[kv[:i]] = kv[i+1:]
				break
			}
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

	// Attach IO streams
	if opts.Stdout != nil {
		cmd.Stdout = opts.Stdout
	} else {
		cmd.Stdout = os.Stdout
	}
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	} else {
		cmd.Stderr = os.Stderr
	}
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	} else {
		cmd.Stdin = os.Stdin
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("task failed in %s: %w", absDir, err)
	}
	return nil
}
