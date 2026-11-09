package main

import (
	"fmt"
	"strings"
	"time"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v4"
	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/display"
	"github.com/alexgorbatchev/cron-person-cli/internal/history"
	"github.com/spf13/cobra"
)

func newTaskCommand() *cobra.Command {
	cmd := newGroupCommand("task", "Inspect, trigger, stop, and pause scheduled tasks")

	cmd.AddCommand(newTaskListCommand())
	cmd.AddCommand(newTaskRunCommand())
	cmd.AddCommand(newTaskHistoryCommand())
	cmd.AddCommand(newTaskLogCommand())
	cmd.AddCommand(newTaskStopCommand())
	cmd.AddCommand(newTaskPauseCommand())
	cmd.AddCommand(newTaskResumeCommand())

	return cmd
}

func newTaskListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tasks defined across all authorized directories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			ready, skipped, err := st.Schedulable()
			if err != nil {
				return err
			}
			warnSkipped(cmd, skipped)

			type taskItem struct {
				Dir         string
				Path        string
				Schedule    string
				Description string
				Command     string
				Line        int
				TaskID      string
				PausedUntil time.Time
			}

			log := runHistory(st)
			now := time.Now()

			var tasks []taskItem
			for _, dir := range ready {
				rec, parsed := dir.Record, dir.File

				for _, t := range parsed.Tasks {
					description, err := cronrc.DescribeSchedule(t.Schedule)
					if err != nil {
						return fmt.Errorf("describing schedule at %s:%d: %w", parsed.Path, t.LineNumber, err)
					}
					item := taskItem{
						Dir:         rec.Dir,
						Path:        parsed.Path,
						Schedule:    t.Schedule,
						Description: description,
						Command:     t.Command,
						Line:        t.LineNumber,
						TaskID:      history.TaskID(rec.Dir, t.Command),
					}
					// A pause record that cannot be read counts as no pause, as it
					// does when the task runs, so it never hides other tasks.
					until, paused, err := log.PausedUntil(item.TaskID, now)
					if err != nil {
						writeWarning(cmd, fmt.Sprintf("ignoring the pause of %s in %s: %s", t.Command, display.Path(rec.Dir), display.Error(err)))
					}
					if paused {
						item.PausedUntil = until
					}
					tasks = append(tasks, item)
				}
			}

			var out strings.Builder
			if cobrahelptree.IsAgentMode() {
				for _, t := range tasks {
					pausedUntil := "-"
					if !t.PausedUntil.IsZero() {
						pausedUntil = t.PausedUntil.UTC().Format(time.RFC3339)
					}
					fmt.Fprintf(&out, "dir: %s\tfile: %s\tline: %d\ttask: %s\tschedule: %s\tdescription: %s\tpaused_until: %s\tcmd: %s\n", display.Path(t.Dir), display.Path(t.Path), t.Line, t.TaskID, t.Schedule, t.Description, pausedUntil, t.Command)
				}
				return writeOutput(cmd, out.String())
			}

			if len(tasks) == 0 {
				return writeOutput(cmd, "No tasks found in authorized directories.\n")
			}

			fmt.Fprintf(&out, "Total scheduled tasks: %d\n\n", len(tasks))
			currentDir := ""
			for _, t := range tasks {
				if t.Dir != currentDir {
					currentDir = t.Dir
					fmt.Fprintf(&out, "Directory: %s\n", display.Path(currentDir))
				}
				fmt.Fprintf(&out, "  %s [%s]\n", t.Description, t.Schedule)
				fmt.Fprintf(&out, "    Source: %s:%d\n", display.Path(t.Path), t.Line)
				fmt.Fprintf(&out, "    Command: %s\n", t.Command)
				fmt.Fprintf(&out, "    Task: %s\n", t.TaskID)
				if !t.PausedUntil.IsZero() {
					fmt.Fprintf(&out, "    Paused until: %s\n", t.PausedUntil.Local().Format(humanTimeLayout))
				}
			}
			return writeOutput(cmd, out.String())
		},
	}
}

func newTaskRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run <dir> <command...>",
		Short: "Manually execute a task inside a target directory with its .cronrc environment",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAttached(cmd, args[0], strings.Join(args[1:], " "))
		},
	}
}
