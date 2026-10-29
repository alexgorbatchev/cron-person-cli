package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/alexgorbatchev/cron-person-cli/internal/display"
	"github.com/alexgorbatchev/cron-person-cli/internal/history"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
	"github.com/spf13/cobra"
)

const (
	defaultHistoryLimit = 20
	humanTimeLayout     = "2006-01-02 15:04:05 MST"
	followInterval      = 200 * time.Millisecond
)

func runHistory(st *store.Store) *history.Log {
	return history.New(history.DirForStore(st.Path()))
}

func newTaskHistoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history [dir]",
		Short: "List recent task runs, newest first",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}
			filter := history.Filter{}
			if len(args) == 1 {
				if filter.Dir, err = filepath.Abs(args[0]); err != nil {
					return fmt.Errorf("resolving directory %s: %w", args[0], err)
				}
			}
			if filter.TaskID, err = cmd.Flags().GetString("task"); err != nil {
				return err
			}
			if filter.Limit, err = cmd.Flags().GetInt("limit"); err != nil {
				return err
			}
			if filter.Limit < 0 {
				return fmt.Errorf("--limit must be zero or greater, got %d", filter.Limit)
			}

			runs, err := runHistory(st).List(filter)
			if err != nil {
				return err
			}

			var out strings.Builder
			if cobrahelptree.IsAgentMode() {
				for _, run := range runs {
					fmt.Fprintf(&out, "id: %s\ttask: %s\tstatus: %s\texit: %s\ttrigger: %s\tstarted: %s\tduration_ms: %d\twarnings: %d\tdir: %s\tcmd: %s\n",
						run.ID, run.TaskID, run.Status, exitCodeText(run), run.Trigger,
						run.StartedAt.Format(time.RFC3339), run.Duration().Milliseconds(), len(run.Warnings), display.Path(run.Dir), run.Command)
				}
				return writeOutput(cmd, out.String())
			}

			if len(runs) == 0 {
				return writeOutput(cmd, "No recorded task runs.\n")
			}
			fmt.Fprintln(&out, "Task Runs:")
			for _, run := range runs {
				fmt.Fprintf(&out, "  %s %s  %s\n", statusTag(run.Status), run.ID, statusText(run))
				fmt.Fprintf(&out, "     Started: %s  Duration: %s  Trigger: %s%s\n",
					run.StartedAt.Local().Format(humanTimeLayout), durationText(run), run.Trigger, warningsText(run))
				fmt.Fprintf(&out, "     Task: %s  %s  %s\n", run.TaskID, display.Path(run.Dir), run.Command)
			}
			return writeOutput(cmd, out.String())
		},
	}
	cmd.Flags().String("task", "", "Show only runs of this task ID")
	cmd.Flags().Int("limit", defaultHistoryLimit, "Maximum number of runs to show (0 shows all)")
	return cmd
}

func newTaskLogCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log <run-id>",
		Short: "Show the details and captured output of a task run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}
			tail, err := cmd.Flags().GetInt("tail")
			if err != nil {
				return err
			}
			if tail < 0 {
				return fmt.Errorf("--tail must be zero or greater, got %d", tail)
			}
			follow, err := cmd.Flags().GetBool("follow")
			if err != nil {
				return err
			}

			log := runHistory(st)
			run, err := log.Get(args[0])
			if errors.Is(err, history.ErrNotFound) {
				return fmt.Errorf("%w: run 'cron-person task history' to list recorded runs", err)
			}
			if err != nil {
				return err
			}

			agent := cobrahelptree.IsAgentMode()
			var out strings.Builder
			writeRunHeader(&out, run, agent)
			if follow {
				if err := writeOutput(cmd, out.String()); err != nil {
					return err
				}
				final, err := log.Follow(cmd.Context(), cmd.OutOrStdout(), run, tail, followInterval)
				if err != nil {
					return err
				}
				return writeOutput(cmd, runFooter(final, agent))
			}

			var output strings.Builder
			if err := log.WriteOutput(&output, run, tail); err != nil {
				return err
			}
			if output.Len() == 0 && !agent {
				output.WriteString("  (no output captured)\n")
			}
			out.WriteString(output.String())
			return writeOutput(cmd, out.String())
		},
	}
	cmd.Flags().Int("tail", 0, "Show only the last N output lines (0 shows all)")
	cmd.Flags().BoolP("follow", "f", false, "Keep printing output as it is captured until the run finishes")
	return cmd
}

// writeRunHeader describes a run ahead of its output.
func writeRunHeader(out *strings.Builder, run history.Run, agent bool) {
	if agent {
		fmt.Fprintf(out, "id: %s\ntask: %s\nstatus: %s\nexit: %s\ntrigger: %s\nstarted: %s\nduration_ms: %d\ndir: %s\ncmd: %s\n",
			run.ID, run.TaskID, run.Status, exitCodeText(run), run.Trigger,
			run.StartedAt.Format(time.RFC3339), run.Duration().Milliseconds(), display.Path(run.Dir), run.Command)
		if run.Error != "" {
			fmt.Fprintf(out, "error: %s\n", display.Error(errors.New(run.Error)))
		}
		for _, w := range run.Warnings {
			fmt.Fprintf(out, "warning: %s\n", display.Error(errors.New(w)))
		}
		if run.Truncated {
			fmt.Fprintln(out, "truncated: true")
		}
		fmt.Fprintln(out, "output:")
		return
	}
	fmt.Fprintf(out, "Run:        %s\n", run.ID)
	fmt.Fprintf(out, "Task:       %s\n", run.TaskID)
	fmt.Fprintf(out, "Directory:  %s\n", display.Path(run.Dir))
	fmt.Fprintf(out, "Command:    %s\n", run.Command)
	fmt.Fprintf(out, "Trigger:    %s\n", run.Trigger)
	fmt.Fprintf(out, "Status:     %s\n", statusText(run))
	if run.Error != "" {
		fmt.Fprintf(out, "Error:      %s\n", display.Error(errors.New(run.Error)))
	}
	for _, w := range run.Warnings {
		fmt.Fprintf(out, "Warning:    %s\n", display.Error(errors.New(w)))
	}
	if run.Truncated {
		fmt.Fprintln(out, "Truncated:  output beyond history.max_run_mb was not stored")
	}
	fmt.Fprintf(out, "Started:    %s\n", run.StartedAt.Local().Format(humanTimeLayout))
	fmt.Fprintf(out, "Duration:   %s\n\n", durationText(run))
	fmt.Fprintln(out, "Output:")
}

// runFooter reports how a followed run ended. Agent output lines are prefixed
// by stream, so trailing keys cannot be mistaken for captured output.
func runFooter(run history.Run, agent bool) string {
	if agent {
		return fmt.Sprintf("final_status: %s\nfinal_exit: %s\nfinal_duration_ms: %d\n", run.Status, exitCodeText(run), run.Duration().Milliseconds())
	}
	return fmt.Sprintf("\nFinished:   %s after %s\n", statusText(run), durationText(run))
}

func statusTag(s history.Status) string {
	switch s {
	case history.StatusSucceeded:
		return "[OK]"
	case history.StatusRunning:
		return "[INFO]"
	default:
		return "[ERROR]"
	}
}

func statusText(run history.Run) string {
	if run.ExitCode != nil && run.Status == history.StatusFailed {
		return fmt.Sprintf("%s (exit %d)", run.Status, *run.ExitCode)
	}
	return string(run.Status)
}

func exitCodeText(run history.Run) string {
	if run.ExitCode == nil {
		return "-"
	}
	return fmt.Sprint(*run.ExitCode)
}

func warningsText(run history.Run) string {
	if len(run.Warnings) == 0 {
		return ""
	}
	return fmt.Sprintf("  Warnings: %d (see task log)", len(run.Warnings))
}

func durationText(run history.Run) string {
	if run.FinishedAt.IsZero() {
		return "-"
	}
	return run.Duration().Round(time.Millisecond).String()
}
