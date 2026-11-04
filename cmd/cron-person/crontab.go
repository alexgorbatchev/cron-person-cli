package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v4"
	"github.com/alexgorbatchev/cron-person-cli/internal/config"
	"github.com/alexgorbatchev/cron-person-cli/internal/crontab"
	"github.com/alexgorbatchev/cron-person-cli/internal/display"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
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

			count, skipped, err := crontab.Sync(st, exePath, configPath)
			if err != nil {
				return fmt.Errorf("syncing crontab: %w", err)
			}
			warnSkipped(cmd, skipped)

			if cobrahelptree.IsAgentMode() {
				return writeOutput(cmd, fmt.Sprintf("synced_directories: %d\nskipped_directories: %d\nstatus: success\n", count, len(skipped)))
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

			block, _, skipped, err := crontab.Preview(st, exePath, configPath)
			if err != nil {
				return err
			}
			warnSkipped(cmd, skipped)

			return writeOutput(cmd, block+"\n")
		},
	}
}

// warnSkipped names each tracked directory a command left out, and why, on
// stderr, so a shorter result is never silent.
func warnSkipped(cmd *cobra.Command, skipped []store.Skipped) {
	for _, s := range skipped {
		warning := fmt.Sprintf("skipped %s: %s", display.Path(s.Record.Dir), display.Error(s.Problem))
		writeWarning(cmd, singleLine.Replace(warning))
	}
}

// singleLine escapes the characters a path may carry that would split a
// warning across lines.
var singleLine = strings.NewReplacer("\r", `\r`, "\n", `\n`, "\x00", `\x00`)

func newCrontabUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove cron-person managed block from system crontab",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The store is not loaded, so a damaged one cannot block removal.
			storePath, err := getStorePath(cmd)
			if err != nil {
				return err
			}
			if err := crontab.Uninstall(filepath.Dir(storePath)); err != nil {
				return fmt.Errorf("uninstalling crontab block: %w", err)
			}

			if cobrahelptree.IsAgentMode() {
				return writeOutput(cmd, "crontab: uninstalled\n")
			}

			return writeOutput(cmd, "[OK] Removed cron-person managed block from system crontab\n")
		},
	}
}
