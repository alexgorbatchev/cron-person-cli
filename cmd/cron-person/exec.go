package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/alexgorbatchev/cron-person-cli/internal/runner"
)

func newExecCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "exec <dir> [--] <command...>",
		Short: "Execute a command in an authorized directory with cwd and .cronrc environment",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			if len(args) < 2 {
				return fmt.Errorf("usage: cron-person exec <dir> [--] <command...>")
			}

			dir, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("resolving directory %s: %w", args[0], err)
			}

			cmdArgs := args[1:]
			if len(cmdArgs) > 0 && cmdArgs[0] == "--" {
				cmdArgs = cmdArgs[1:]
			}
			if len(cmdArgs) == 0 {
				return fmt.Errorf("no command provided to execute")
			}

			command := strings.Join(cmdArgs, " ")
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
