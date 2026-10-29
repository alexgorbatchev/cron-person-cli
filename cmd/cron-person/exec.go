package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/alexgorbatchev/cron-person-cli/internal/config"
	"github.com/alexgorbatchev/cron-person-cli/internal/history"
	"github.com/alexgorbatchev/cron-person-cli/internal/runner"
	"github.com/spf13/cobra"
)

const (
	triggerFlag   = "trigger"
	runIDFileFlag = "run-id-file"
)

func newExecCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <dir> [--] <command...>",
		Short: "Execute a command in an authorized directory with cwd and .cronrc environment",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdArgs := args[1:]
			if len(cmdArgs) > 0 && cmdArgs[0] == "--" {
				cmdArgs = cmdArgs[1:]
			}
			if len(cmdArgs) == 0 {
				return fmt.Errorf("no command provided to execute")
			}
			trigger, err := cmd.Flags().GetString(triggerFlag)
			if err != nil {
				return err
			}
			if trigger != string(history.TriggerCron) && trigger != string(history.TriggerManual) {
				return fmt.Errorf("--%s must be %s or %s, got %q", triggerFlag, history.TriggerCron, history.TriggerManual, trigger)
			}
			runIDFile, err := cmd.Flags().GetString(runIDFileFlag)
			if err != nil {
				return err
			}
			return runRecordedTask(cmd, args[0], strings.Join(cmdArgs, " "), history.Trigger(trigger), runIDFile)
		},
	}
	// Set by a foreground `task run`, which delegates the run to this command.
	cmd.Flags().String(triggerFlag, string(history.TriggerCron), "Trigger recorded with the run")
	cmd.Flags().String(runIDFileFlag, "", "File that receives the run ID once recording starts")
	_ = cmd.Flags().MarkHidden(triggerFlag)
	_ = cmd.Flags().MarkHidden(runIDFileFlag)
	return cmd
}

// runRecordedTask executes command in dir, records the run, and enforces the
// configured history limits. An unreadable configuration never stops the task
// or fails a cron run: the defaults apply and the problem is recorded with the
// run, and a manual run also prints it.
//
// SIGINT, SIGTERM and SIGHUP terminate the task's process group gracefully, so
// the outcome is still recorded. SIGPIPE is observed rather than fatal: a
// foreground `task run` that detaches closes the streams this process writes to,
// and capture must outlive it.
func runRecordedTask(cmd *cobra.Command, dir, command string, trigger history.Trigger, runIDFile string) error {
	st, err := getStore(cmd)
	if err != nil {
		return err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving directory %s: %w", dir, err)
	}

	var warnings []string
	cfg, cfgErr := loadConfig(cmd)
	if cfgErr != nil {
		cfg = config.Default()
		warning := "using default settings: " + cfgErr.Error()
		warnings = append(warnings, warning)
		if trigger == history.TriggerManual {
			writeWarning(cmd, warning)
		}
	}

	pipes := make(chan os.Signal, 1)
	signal.Notify(pipes, syscall.SIGPIPE)
	defer signal.Stop(pipes)
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	var idErr error
	opts := runner.ExecOptions{
		Dir:            absDir,
		Command:        command,
		Stdout:         cmd.OutOrStdout(),
		Stderr:         cmd.ErrOrStderr(),
		Stdin:          cmd.InOrStdin(),
		History:        runHistory(st),
		Trigger:        trigger,
		Retention:      cfg.History.Retention(),
		MaxOutputBytes: cfg.History.MaxOutputBytes(),
		Warnings:       warnings,
	}
	if runIDFile != "" {
		opts.OnStart = func(id string) {
			if err := os.WriteFile(runIDFile, []byte(id), 0o600); err != nil {
				idErr = fmt.Errorf("reporting run ID: %w", err)
			}
		}
	}
	return errors.Join(runner.NewRunner(st).Exec(ctx, opts), idErr)
}

func writeWarning(cmd *cobra.Command, msg string) {
	if cobrahelptree.IsAgentMode() {
		fmt.Fprintf(cmd.ErrOrStderr(), "WARN: %s\n", msg)
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "[WARN] %s\n", msg)
}
