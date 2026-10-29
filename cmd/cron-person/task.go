package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/display"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
	"github.com/spf13/cobra"
)

func newTaskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Inspect and trigger scheduled tasks",
	}

	cmd.AddCommand(newTaskListCommand())
	cmd.AddCommand(newTaskRunCommand())
	cmd.AddCommand(newTaskHistoryCommand())
	cmd.AddCommand(newTaskLogCommand())

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

			records, err := st.List()
			if err != nil {
				return err
			}

			type taskItem struct {
				Dir         string
				Path        string
				Schedule    string
				Description string
				Command     string
				Line        int
			}

			var tasks []taskItem
			for _, rec := range records {
				status, _, snapshot, err := st.Inspect(rec.Dir)
				if err != nil {
					return fmt.Errorf("checking authorization for %s: %w", rec.Dir, err)
				}
				if status != store.StatusAllowed {
					continue
				}

				path := filepath.Join(rec.Dir, ".cronrc")
				parsed, err := cronrc.Parse(bytes.NewReader(snapshot), path)
				if err != nil {
					return fmt.Errorf("parsing %s: %w", path, err)
				}

				for _, t := range parsed.Tasks {
					description, err := cronrc.DescribeSchedule(t.Schedule)
					if err != nil {
						return fmt.Errorf("describing schedule at %s:%d: %w", parsed.Path, t.LineNumber, err)
					}
					tasks = append(tasks, taskItem{
						Dir:         rec.Dir,
						Path:        parsed.Path,
						Schedule:    t.Schedule,
						Description: description,
						Command:     t.Command,
						Line:        t.LineNumber,
					})
				}
			}

			var out strings.Builder
			if cobrahelptree.IsAgentMode() {
				for _, t := range tasks {
					fmt.Fprintf(&out, "dir: %s\tfile: %s\tline: %d\tschedule: %s\tdescription: %s\tcmd: %s\n", display.Path(t.Dir), display.Path(t.Path), t.Line, t.Schedule, t.Description, t.Command)
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
