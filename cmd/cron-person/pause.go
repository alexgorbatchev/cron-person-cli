package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v4"
	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/display"
	"github.com/alexgorbatchev/cron-person-cli/internal/history"
	"github.com/alexgorbatchev/cron-person-cli/internal/runner"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
	"github.com/spf13/cobra"
)

const pauseForFlag = "for"

// authorizedTask resolves dir and command to a task in the directory's
// authorized .cronrc, and returns the directory as the store records it,
// which is the directory scheduled runs name.
func authorizedTask(st *store.Store, dir, command string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving directory %s: %w", dir, err)
	}
	status, rec, snapshot, err := st.Inspect(absDir)
	if err != nil {
		return "", fmt.Errorf("checking authorization for %s: %w", absDir, err)
	}
	if status != store.StatusAllowed {
		return "", fmt.Errorf("directory %s is %s: only tasks in an authorized .cronrc can be paused", display.Path(absDir), status)
	}
	parsed, err := cronrc.Parse(bytes.NewReader(snapshot), rec.Path)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", display.Path(rec.Path), err)
	}
	for _, t := range parsed.Tasks {
		if t.Command == command {
			return rec.Dir, nil
		}
	}
	return "", fmt.Errorf("no task with command %q in %s: run 'cron-person task list' to see task commands", command, display.Path(rec.Path))
}

func newTaskPauseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pause <dir> <command...>",
		Short: "Stop a task's live runs and skip its scheduled runs for a duration",
		Long: "Stop a task's live runs and skip its scheduled runs for a duration. The task is named by its directory and " +
			"its exact command, as 'cron-person task list' shows them. Scheduled runs during the pause are recorded as paused " +
			"and run nothing; a manual 'task run' still runs, with a warning. Pausing a paused task replaces the end time. " +
			"The crontab is unchanged, so the pause ends on its own.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			duration, err := cmd.Flags().GetDuration(pauseForFlag)
			if err != nil {
				return err
			}
			if duration <= 0 {
				return fmt.Errorf("--%s must be a positive duration such as 90m or 2h, got %s", pauseForFlag, duration)
			}
			st, err := getStore(cmd)
			if err != nil {
				return err
			}
			command := strings.Join(args[1:], " ")
			dir, err := authorizedTask(st, args[0], command)
			if err != nil {
				return err
			}
			log := runHistory(st)
			until := time.Now().Add(duration)
			if err := log.Pause(dir, command, until); err != nil {
				return err
			}
			results, err := runner.StopTask(cmd.Context(), log, history.TaskID(dir, command))
			if err != nil {
				return err
			}

			agent := cobrahelptree.IsAgentMode()
			var out strings.Builder
			if agent {
				fmt.Fprintf(&out, "dir: %s\ttask: %s\tpaused_until: %s\tcmd: %s\n", display.Path(dir), history.TaskID(dir, command), until.UTC().Format(time.RFC3339), command)
			} else {
				fmt.Fprintf(&out, "[OK] Paused until %s\n     Task: %s  %s\n", until.Local().Format(humanTimeLayout), display.Path(dir), command)
			}
			errs := writeStopResults(&out, results, agent)
			if err := writeOutput(cmd, out.String()); err != nil {
				return err
			}
			return errors.Join(errs...)
		},
	}
	cmd.Flags().Duration(pauseForFlag, 0, "How long to pause the task, such as 90m or 2h (required)")
	_ = cmd.MarkFlagRequired(pauseForFlag)
	return cmd
}

func newTaskResumeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <dir> <command...>",
		Short: "End a task's pause so its scheduled runs start again",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}
			command := strings.Join(args[1:], " ")
			absDir, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("resolving directory %s: %w", args[0], err)
			}
			// A pause outlives its directory's authorization, so resuming
			// needs only the recorded directory, not an authorized .cronrc.
			dir := absDir
			if rec, ok, err := st.Get(absDir); err != nil {
				return err
			} else if ok {
				dir = rec.Dir
			}
			if err := runHistory(st).Resume(dir, command, time.Now()); err != nil {
				if errors.Is(err, history.ErrNotPaused) {
					return fmt.Errorf("%w: %s  %s", err, display.Path(dir), command)
				}
				return err
			}
			if cobrahelptree.IsAgentMode() {
				return writeOutput(cmd, fmt.Sprintf("dir: %s\ttask: %s\tstatus: resumed\tcmd: %s\n", display.Path(dir), history.TaskID(dir, command), command))
			}
			return writeOutput(cmd, fmt.Sprintf("[OK] Resumed\n     Task: %s  %s\n", display.Path(dir), command))
		},
	}
}
