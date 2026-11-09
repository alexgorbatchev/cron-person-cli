package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v4"
	"github.com/alexgorbatchev/cron-person-cli/internal/display"
	"github.com/alexgorbatchev/cron-person-cli/internal/history"
	"github.com/alexgorbatchev/cron-person-cli/internal/runner"
	"github.com/spf13/cobra"
)

// stopWaitMargin is how long `task stop` waits beyond the grace period for a
// recorder to notice the request and record the outcome.
const stopWaitMargin = 5 * time.Second

// stopTimeout bounds how long stopRuns waits for the runs it asked to stop.
// Tests shorten it.
var stopTimeout = runner.DefaultGrace + stopWaitMargin

// stopResult is the outcome of asking one run to stop.
type stopResult struct {
	ID  string
	Run history.Run
	Err error
}

// stopRuns asks every run in ids to stop, then waits for all of them, so the
// runs stop concurrently and the wait is bounded by stopTimeout as a whole.
func stopRuns(ctx context.Context, log *history.Log, ids []string) []stopResult {
	results := make([]stopResult, len(ids))
	for i, id := range ids {
		run, err := log.RequestStop(id)
		if errors.Is(err, history.ErrNotFound) {
			err = fmt.Errorf("%w: run 'cron-person task history' to list recorded runs", err)
		}
		results[i] = stopResult{ID: id, Run: run, Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, stopTimeout)
	defer cancel()
	for i := range results {
		r := &results[i]
		if r.Err != nil {
			continue
		}
		final, err := log.Wait(ctx, r.Run, followInterval)
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			r.Err = fmt.Errorf("run %s is still running %s after the stop request", r.ID, stopTimeout)
		case err != nil:
			r.Err = err
		default:
			r.Run = final
		}
	}
	return results
}

func newTaskStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <run-id>...",
		Short: "Stop live task runs and wait for each to finish",
		Long: "Stop live task runs and wait for each to finish. Each run's recorder ends its task with SIGTERM, " +
			"kills the task's processes if they are still running after the grace period, and records the run as stopped. " +
			"Find run IDs with 'cron-person task history'.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}
			results := stopRuns(cmd.Context(), runHistory(st), args)

			agent := cobrahelptree.IsAgentMode()
			var out strings.Builder
			var errs []error
			for _, r := range results {
				if r.Err != nil {
					errs = append(errs, r.Err)
					continue
				}
				if agent {
					fmt.Fprintf(&out, "id: %s\tstatus: %s\texit: %s\tdir: %s\tcmd: %s\n", r.Run.ID, r.Run.Status, exitCodeText(r.Run), display.Path(r.Run.Dir), r.Run.Command)
					continue
				}
				fmt.Fprintf(&out, "%s %s  %s\n     Task: %s  %s\n", statusTag(r.Run.Status), r.Run.ID, statusText(r.Run), display.Path(r.Run.Dir), r.Run.Command)
			}
			if err := writeOutput(cmd, out.String()); err != nil {
				return err
			}
			return errors.Join(errs...)
		},
	}
}
