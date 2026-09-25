package main

import (
	"fmt"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/spf13/cobra"
	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

var (
	version = "0.1.0"
)

func getStore(cmd *cobra.Command) (*store.Store, error) {
	storePath, _ := cmd.Flags().GetString("store")
	if storePath == "" {
		var err error
		storePath, err = store.DefaultStorePath()
		if err != nil {
			return nil, fmt.Errorf("resolving store path: %w", err)
		}
	}
	return store.NewStore(storePath)
}

func newRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "cron-person",
		Short:        "Higher-order per-directory crontab manager with direnv-style shell integration",
		Long:         "cron-person is a higher-order crontab manager that executes cron tasks in their respective directory contexts with direnv-style allow/deny authorization.",
		Version:      version,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().String("store", "", "Path to custom authorization state file (default: $XDG_STATE_HOME/cron-person/allowed.json)")

	// Subcommands
	rootCmd.AddCommand(newDirCommand())
	rootCmd.AddCommand(newCrontabCommand())
	rootCmd.AddCommand(newTaskCommand())
	rootCmd.AddCommand(newExecCommand())
	rootCmd.AddCommand(newHookCommand())
	rootCmd.AddCommand(newRootStatusCommand())

	// Top-level aliases
	rootCmd.AddCommand(newDirAllowCommand())
	rootCmd.AddCommand(newDirDenyCommand())

	// Clean version output contract
	rootCmd.SetVersionTemplate("{{.Version}}\n")

	setupHelp(rootCmd)

	return rootCmd
}

func newRootStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Display overall system status and tracked directory metrics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			records := st.List()
			allowedCount := 0
			totalTasks := 0

			for _, rec := range records {
				status, _, err := st.Status(rec.Dir)
				if err == nil && status == store.StatusAllowed {
					allowedCount++
					if parsed, err := cronrc.ParseFile(rec.Path); err == nil {
						totalTasks += len(parsed.Tasks)
					}
				}
			}

			if cobrahelptree.IsAgentMode() {
				fmt.Fprintln(cmd.OutOrStdout(), "version: "+version)
				fmt.Fprintf(cmd.OutOrStdout(), "tracked_dirs: %d\n", len(records))
				fmt.Fprintf(cmd.OutOrStdout(), "allowed_dirs: %d\n", allowedCount)
				fmt.Fprintf(cmd.OutOrStdout(), "total_tasks: %d\n", totalTasks)
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), "[OK] cron-person system status: active")
			fmt.Fprintf(cmd.OutOrStdout(), "  Version:              %s\n", version)
			fmt.Fprintf(cmd.OutOrStdout(), "  Tracked Directories:  %d\n", len(records))
			fmt.Fprintf(cmd.OutOrStdout(), "  Allowed Directories:  %d\n", allowedCount)
			fmt.Fprintf(cmd.OutOrStdout(), "  Active Tasks:         %d\n", totalTasks)
			return nil
		},
	}
}
