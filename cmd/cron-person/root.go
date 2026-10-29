package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v3"
	"github.com/alexgorbatchev/cron-person-cli/internal/config"
	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
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

			records, err := st.List()
			if err != nil {
				return err
			}
			allowedCount := 0
			totalTasks := 0

			for _, rec := range records {
				status, _, snapshot, err := st.Inspect(rec.Dir)
				if err != nil {
					return fmt.Errorf("checking authorization for %s: %w", rec.Dir, err)
				}
				if status == store.StatusAllowed {
					allowedCount++
					path := filepath.Join(rec.Dir, ".cronrc")
					parsed, err := cronrc.Parse(bytes.NewReader(snapshot), path)
					if err != nil {
						return fmt.Errorf("parsing %s: %w", path, err)
					}
					totalTasks += len(parsed.Tasks)
				}
			}

			configPath, err := getConfigPath(cmd)
			if err != nil {
				return err
			}
			configState, configErr := describeConfig(configPath)

			var out strings.Builder
			if cobrahelptree.IsAgentMode() {
				fmt.Fprintln(&out, "version: "+version)
				fmt.Fprintf(&out, "tracked_dirs: %d\n", len(records))
				fmt.Fprintf(&out, "allowed_dirs: %d\n", allowedCount)
				fmt.Fprintf(&out, "total_tasks: %d\n", totalTasks)
				fmt.Fprintf(&out, "config: %s\n", display.Path(configPath))
				fmt.Fprintf(&out, "config_status: %s\n", configState)
				if configErr != nil {
					fmt.Fprintf(&out, "config_error: %s\n", display.Error(configErr))
				}
				return writeOutput(cmd, out.String())
			}

			if configErr != nil {
				fmt.Fprintln(&out, "[WARN] cron-person system status: configuration invalid, defaults in use")
			} else {
				fmt.Fprintln(&out, "[OK] cron-person system status: active")
			}
			fmt.Fprintf(&out, "  Version:              %s\n", version)
			fmt.Fprintf(&out, "  Tracked Directories:  %d\n", len(records))
			fmt.Fprintf(&out, "  Allowed Directories:  %d\n", allowedCount)
			fmt.Fprintf(&out, "  Active Tasks:         %d\n", totalTasks)
			fmt.Fprintf(&out, "  Configuration:        %s (%s)\n", display.Path(configPath), configState)
			if configErr != nil {
				fmt.Fprintf(&out, "  Configuration Error:  %s\n", display.Error(configErr))
			}
			return writeOutput(cmd, out.String())
		},
	}
}
