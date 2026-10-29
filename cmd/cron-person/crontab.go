package main

import (
	"fmt"
	"os"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/alexgorbatchev/cron-person-cli/internal/config"
	"github.com/alexgorbatchev/cron-person-cli/internal/crontab"
	"github.com/spf13/cobra"
)

func newCrontabCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crontab",
		Short: "Synchronize and inspect system crontab integration",
	}

	cmd.AddCommand(newCrontabSyncCommand())
	cmd.AddCommand(newCrontabShowCommand())
	cmd.AddCommand(newCrontabUninstallCommand())

	return cmd
}

func newCrontabSyncCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Compile authorized directory jobs and install into user crontab",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			exePath, err := os.Executable()
			if err != nil {
				exePath = "cron-person"
			}

			configPath, err := getConfigPath(cmd)
			if err != nil {
				return err
			}
			// Scheduled runs fall back to defaults quietly, so a broken configuration
			// is caught here, when schedules are installed.
			if _, err := config.Load(configPath); err != nil {
				return fmt.Errorf("fix the configuration before syncing: %w", err)
			}

			count, err := crontab.Sync(st, exePath, configPath)
			if err != nil {
				return fmt.Errorf("syncing crontab: %w", err)
			}

			if cobrahelptree.IsAgentMode() {
				return writeOutput(cmd, fmt.Sprintf("synced_directories: %d\nstatus: success\n", count))
			}

			return writeOutput(cmd, fmt.Sprintf("[OK] Synchronized %d authorized directory task(s) into crontab\n", count))
		},
	}
}

func newCrontabShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Preview generated crontab block without modifying crontab",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			exePath, err := os.Executable()
			if err != nil {
				exePath = "cron-person"
			}

			configPath, err := getConfigPath(cmd)
			if err != nil {
				return err
			}

			block, _, err := crontab.Preview(st, exePath, configPath)
			if err != nil {
				return err
			}

			return writeOutput(cmd, block+"\n")
		},
	}
}

func newCrontabUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove cron-person managed block from system crontab",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := crontab.Uninstall(); err != nil {
				return fmt.Errorf("uninstalling crontab block: %w", err)
			}

			if cobrahelptree.IsAgentMode() {
				return writeOutput(cmd, "crontab: uninstalled\n")
			}

			return writeOutput(cmd, "[OK] Removed cron-person managed block from system crontab\n")
		},
	}
}
