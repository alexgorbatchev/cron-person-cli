package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v4"
	"github.com/alexgorbatchev/cron-person-cli/internal/config"
	"github.com/alexgorbatchev/cron-person-cli/internal/display"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
	"github.com/spf13/cobra"
)

var (
	version = "0.0.1"
)

func writeOutput(cmd *cobra.Command, s string) error {
	_, err := fmt.Fprint(cmd.OutOrStdout(), s)
	return err
}

func getStore(cmd *cobra.Command) (*store.Store, error) {
	path, err := getStorePath(cmd)
	if err != nil {
		return nil, err
	}
	return store.NewStore(path)
}

// getStorePath resolves the authorization store path without loading it.
func getStorePath(cmd *cobra.Command) (string, error) {
	storePath, _ := cmd.Flags().GetString("store")
	if storePath == "" {
		var err error
		storePath, err = store.DefaultStorePath()
		if err != nil {
			return "", fmt.Errorf("resolving store path: %w", err)
		}
	}
	return filepath.Abs(storePath)
}

func getConfigPath(cmd *cobra.Command) (string, error) {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		var err error
		if configPath, err = config.DefaultPath(); err != nil {
			return "", fmt.Errorf("resolving config path: %w", err)
		}
	}
	return filepath.Abs(configPath)
}

func loadConfig(cmd *cobra.Command) (config.Config, error) {
	path, err := getConfigPath(cmd)
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(path)
}

// describeConfig reports whether the configuration file is absent (defaults),
// loaded, or invalid, with the reason when it is invalid.
func describeConfig(path string) (string, error) {
	if _, err := config.Load(path); err != nil {
		return "invalid", err
	}
	if _, err := os.Stat(path); err == nil {
		return "loaded", nil
	}
	return "defaults", nil
}

// newGroupCommand returns a command that only holds subcommands: without
// arguments it prints its help, and an unknown subcommand is an error rather
// than a silent help screen.
func newGroupCommand(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
}

func newRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "cron-person",
		Short:         "Higher-order per-directory crontab manager with direnv-style shell integration",
		Long:          "cron-person is a higher-order crontab manager that executes cron tasks in their respective directory contexts with direnv-style allow/deny authorization.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().String("store", "", "Path to custom authorization state file (default: $XDG_STATE_HOME/cron-person/allowed.json)")
	rootCmd.PersistentFlags().String("config", "", "Path to custom configuration file (default: $XDG_CONFIG_HOME/cron-person/config.json)")

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

// statusRow is one tracked directory as status reports it.
type statusRow struct {
	record  store.Record
	status  store.DirStatus
	tasks   int
	problem error
}

// statusRows merges ready and skipped directories back into directory order.
func statusRows(ready []store.Directory, skipped []store.Skipped) []statusRow {
	rows := make([]statusRow, 0, len(ready)+len(skipped))
	for _, dir := range ready {
		rows = append(rows, statusRow{record: dir.Record, status: store.StatusAllowed, tasks: len(dir.File.Tasks)})
	}
	for _, s := range skipped {
		rows = append(rows, statusRow{record: s.Record, status: s.Status, problem: s.Problem})
	}
	slices.SortFunc(rows, func(a, b statusRow) int { return strings.Compare(a.record.Dir, b.record.Dir) })
	return rows
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func newRootStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Display overall system status and every tracked directory",
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
			rows := statusRows(ready, skipped)
			allowedCount, totalTasks := len(ready), 0
			for _, dir := range ready {
				totalTasks += len(dir.File.Tasks)
			}

			configPath, err := getConfigPath(cmd)
			if err != nil {
				return err
			}
			configState, configErr := describeConfig(configPath)

			var out strings.Builder
			if cobrahelptree.IsAgentMode() {
				fmt.Fprintln(&out, "version: "+version)
				fmt.Fprintf(&out, "tracked_dirs: %d\n", len(rows))
				fmt.Fprintf(&out, "allowed_dirs: %d\n", allowedCount)
				fmt.Fprintf(&out, "total_tasks: %d\n", totalTasks)
				fmt.Fprintf(&out, "config: %s\n", display.Path(configPath))
				fmt.Fprintf(&out, "config_status: %s\n", configState)
				if configErr != nil {
					fmt.Fprintf(&out, "config_error: %s\n", display.Error(configErr))
				}
				for _, row := range rows {
					fmt.Fprintf(&out, "dir: %s\tstatus: %s\thash: %s", singleLine.Replace(display.Path(row.record.Dir)), row.status, row.record.Hash)
					if row.problem != nil {
						fmt.Fprintf(&out, "\tproblem: %s\n", singleLine.Replace(display.Error(row.problem)))
					} else {
						fmt.Fprintf(&out, "\ttasks: %d\n", row.tasks)
					}
				}
				return writeOutput(cmd, out.String())
			}

			if configErr != nil {
				fmt.Fprintln(&out, "[WARN] cron-person system status: configuration invalid, defaults in use")
			} else {
				fmt.Fprintln(&out, "[OK] cron-person system status: active")
			}
			fmt.Fprintf(&out, "  Version:              %s\n", version)
			fmt.Fprintf(&out, "  Tracked Directories:  %d\n", len(rows))
			fmt.Fprintf(&out, "  Allowed Directories:  %d\n", allowedCount)
			fmt.Fprintf(&out, "  Active Tasks:         %d\n", totalTasks)
			fmt.Fprintf(&out, "  Configuration:        %s (%s)\n", display.Path(configPath), configState)
			if configErr != nil {
				fmt.Fprintf(&out, "  Configuration Error:  %s\n", display.Error(configErr))
			}

			if len(rows) == 0 {
				fmt.Fprintln(&out, "\nNo tracked directories. Use 'cron-person allow <path>' to authorize a directory.")
				return writeOutput(cmd, out.String())
			}
			fmt.Fprintln(&out, "\nTracked Directories:")
			for _, row := range rows {
				dir := singleLine.Replace(display.Path(row.record.Dir))
				if row.problem != nil {
					fmt.Fprintf(&out, "  [WARN] %s (%s)\n", dir, row.status)
				} else {
					fmt.Fprintf(&out, "  [OK] %s (%s, %d task%s)\n", dir, row.status, row.tasks, plural(row.tasks))
				}
				fmt.Fprintf(&out, "     Hash: %s\n", row.record.Hash)
				if row.problem != nil {
					fmt.Fprintf(&out, "     Problem: %s\n", singleLine.Replace(display.Error(row.problem)))
				}
			}
			return writeOutput(cmd, out.String())
		},
	}
}
