package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/alexgorbatchev/cron-person-cli/internal/agent"
	"github.com/alexgorbatchev/cron-person-cli/internal/crontab"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
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

			count, err := crontab.Sync(st, exePath)
			if err != nil {
				return fmt.Errorf("syncing crontab: %w", err)
			}

			if agent.IsAgentMode() {
				fmt.Fprintf(cmd.OutOrStdout(), "synced_directories: %d\nstatus: success\n", count)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "[OK] Synchronized %d authorized directory task(s) into crontab\n", count)
			return nil
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

			records := st.List()
			validRecords := make([]store.Record, 0, len(records))
			for _, rec := range records {
				status, _, err := st.Status(rec.Dir)
				if err == nil && status == store.StatusAllowed {
					validRecords = append(validRecords, rec)
				}
			}

			block, err := crontab.GenerateBlock(validRecords, exePath)
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), block)
			return nil
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

			if agent.IsAgentMode() {
				fmt.Fprintln(cmd.OutOrStdout(), "crontab: uninstalled")
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), "[OK] Removed cron-person managed block from system crontab")
			return nil
		},
	}
}
