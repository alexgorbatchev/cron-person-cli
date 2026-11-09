package main

import (
	"errors"
	"fmt"
	"strings"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v4"
	"github.com/alexgorbatchev/cron-person-cli/internal/display"
	"github.com/alexgorbatchev/cron-person-cli/internal/history"
	"github.com/alexgorbatchev/cron-person-cli/internal/runner"
	"github.com/spf13/cobra"
)

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
			results := runner.StopRuns(cmd.Context(), runHistory(st), args)

			agent := cobrahelptree.IsAgentMode()
			var out strings.Builder
			var errs []error
			for _, r := range results {
				if errors.Is(r.Err, history.ErrNotFound) {
					errs = append(errs, fmt.Errorf("%w: run 'cron-person task history' to list recorded runs", r.Err))
					continue
				}
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
