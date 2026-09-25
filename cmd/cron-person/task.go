package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/alexgorbatchev/cron-person-cli/internal/agent"
	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/runner"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func newTaskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Inspect and trigger scheduled tasks",
	}

	cmd.AddCommand(newTaskListCommand())
	cmd.AddCommand(newTaskRunCommand())

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

			records := st.List()

			type taskItem struct {
				Dir      string
				Schedule string
				Command  string
				Line     int
			}

			var tasks []taskItem
			for _, rec := range records {
				status, _, err := st.Status(rec.Dir)
				if err != nil || status != store.StatusAllowed {
					continue
				}

				parsed, err := cronrc.ParseFile(rec.Path)
				if err != nil {
					continue
				}

				for _, t := range parsed.Tasks {
					tasks = append(tasks, taskItem{
						Dir:      rec.Dir,
						Schedule: t.Schedule,
						Command:  t.Command,
						Line:     t.LineNumber,
					})
				}
			}

			if agent.IsAgentMode() {
				for _, t := range tasks {
					fmt.Fprintf(cmd.OutOrStdout(), "dir: %s\tschedule: %s\tline: %d\tcmd: %s\n", t.Dir, t.Schedule, t.Line, t.Command)
				}
				return nil
			}

			if len(tasks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tasks found in authorized directories.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Total scheduled tasks: %d\n\n", len(tasks))
			currentDir := ""
			for _, t := range tasks {
				if t.Dir != currentDir {
					currentDir = t.Dir
					fmt.Fprintf(cmd.OutOrStdout(), "Directory: %s\n", currentDir)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] (line %d) %s\n", t.Schedule, t.Line, t.Command)
			}
			return nil
		},
	}
}

func newTaskRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run <dir> <command...>",
		Short: "Manually execute a task inside a target directory with its .cronrc environment",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			dir, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("resolving directory %s: %w", args[0], err)
			}

			command := strings.Join(args[1:], " ")
			r := runner.NewRunner(st)

			opts := runner.ExecOptions{
				Dir:     dir,
				Command: command,
				Stdout:  cmd.OutOrStdout(),
				Stderr:  cmd.ErrOrStderr(),
				Stdin:   cmd.InOrStdin(),
			}

			return r.Exec(context.Background(), opts)
		},
	}
}
