package main

import (
	"os"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/alexgorbatchev/cron-person-cli/internal/hook"
	"github.com/spf13/cobra"
)

func newHookCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook <bash|zsh|fish>",
		Short: "Generate shell integration hook script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shellName := args[0]
			script, err := hook.GenerateHookScript(shellName, "cron-person")
			if err != nil {
				return err
			}

			return writeOutput(cmd, script)
		},
	}

	cmd.AddCommand(newHookExportCommand())
	return cmd
}

func newHookExportCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "export [shell]",
		Short:  "Evaluate and inspect directory state on shell prompt hook",
		Hidden: true,
		Args:   cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			return hook.HandleHookExport(st, cwd, cobrahelptree.IsAgentMode(), cmd.ErrOrStderr())
		},
	}
}
