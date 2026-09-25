package main

import (
	"fmt"
	"os"
	"path/filepath"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/spf13/cobra"
	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func resolveTargetDir(args []string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return filepath.Abs(args[0])
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	// Check if there's a .cronrc in cwd or parent
	found, err := cronrc.FindCronrc(cwd)
	if err == nil && found != "" {
		return filepath.Dir(found), nil
	}
	return cwd, nil
}

func newDirCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dir",
		Short: "Inspect and manage directory authorizations and .cronrc configurations",
	}

	cmd.AddCommand(newDirAllowCommand())
	cmd.AddCommand(newDirDenyCommand())
	cmd.AddCommand(newDirListCommand())
	cmd.AddCommand(newDirCheckCommand())
	cmd.AddCommand(newDirStatusCommand())

	return cmd
}

func newDirAllowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "allow [path]",
		Short: "Authorize .cronrc in the specified directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			dir, err := resolveTargetDir(args)
			if err != nil {
				return err
			}

			rec, err := st.Allow(dir)
			if err != nil {
				return fmt.Errorf("allowing directory %s: %w", dir, err)
			}

			if cobrahelptree.IsAgentMode() {
				fmt.Fprintf(cmd.OutOrStdout(), "dir: %s\nstatus: allowed\nhash: %s\n", rec.Dir, rec.Hash)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "[OK] Authorized .cronrc in %s\n", rec.Dir)
			fmt.Fprintf(cmd.OutOrStdout(), "     Hash: %s\n", rec.Hash)
			return nil
		},
	}
}

func newDirDenyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "deny [path]",
		Short: "Revoke authorization for the specified directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			dir, err := resolveTargetDir(args)
			if err != nil {
				return err
			}

			if err := st.Deny(dir); err != nil {
				return fmt.Errorf("denying directory %s: %w", dir, err)
			}

			if cobrahelptree.IsAgentMode() {
				fmt.Fprintf(cmd.OutOrStdout(), "dir: %s\nstatus: denied\n", dir)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "[OK] Revoked authorization for %s\n", dir)
			return nil
		},
	}
}

func newDirListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tracked directories and their authorization status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			records := st.List()

			if cobrahelptree.IsAgentMode() {
				for _, rec := range records {
					status, _, _ := st.Status(rec.Dir)
					fmt.Fprintf(cmd.OutOrStdout(), "dir: %s\tstatus: %s\thash: %s\n", rec.Dir, status, rec.Hash)
				}
				return nil
			}

			if len(records) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tracked directories. Use 'cron-person allow <path>' to authorize a directory.")
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Tracked Directories:")
			for _, rec := range records {
				status, _, _ := st.Status(rec.Dir)
				statusTag := "[OK]"
				if status != store.StatusAllowed {
					statusTag = "[WARN]"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %s %s (%s)\n", statusTag, rec.Dir, status)
				fmt.Fprintf(cmd.OutOrStdout(), "     Hash: %s\n", rec.Hash)
			}
			return nil
		},
	}
}

func newDirCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check [path]",
		Short: "Validate syntax and parseability of a .cronrc file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveTargetDir(args)
			if err != nil {
				return err
			}

			cronrcPath := filepath.Join(dir, ".cronrc")
			parsed, err := cronrc.ParseFile(cronrcPath)
			if err != nil {
				return fmt.Errorf("invalid .cronrc syntax: %w", err)
			}

			if cobrahelptree.IsAgentMode() {
				fmt.Fprintf(cmd.OutOrStdout(), "path: %s\ntasks_count: %d\nhash: %s\n", parsed.Path, len(parsed.Tasks), parsed.Hash)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "[OK] Syntax valid for %s\n", parsed.Path)
			fmt.Fprintf(cmd.OutOrStdout(), "     Found %d scheduled task(s)\n", len(parsed.Tasks))
			if len(parsed.Env) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "     Environment variables: %d defined\n", len(parsed.Env))
			}
			for i, t := range parsed.Tasks {
				fmt.Fprintf(cmd.OutOrStdout(), "     Task #%d (line %d): %s => %s\n", i+1, t.LineNumber, t.Schedule, t.Command)
			}
			return nil
		},
	}
}

func newDirStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status [path]",
		Short: "Show status and tasks for a specific directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := getStore(cmd)
			if err != nil {
				return err
			}

			dir, err := resolveTargetDir(args)
			if err != nil {
				return err
			}

			status, rec, err := st.Status(dir)
			if err != nil {
				return err
			}

			if cobrahelptree.IsAgentMode() {
				fmt.Fprintf(cmd.OutOrStdout(), "dir: %s\nstatus: %s\n", dir, status)
				if rec != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "hash: %s\n", rec.Hash)
				}
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Directory: %s\n", dir)
			fmt.Fprintf(cmd.OutOrStdout(), "Status:    %s\n", status)
			if rec != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Hash:      %s\n", rec.Hash)
				fmt.Fprintf(cmd.OutOrStdout(), "Allowed:   %s\n", rec.AllowedAt.Format("2006-01-02 15:04:05 UTC"))
			}

			cronrcPath := filepath.Join(dir, ".cronrc")
			if parsed, err := cronrc.ParseFile(cronrcPath); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "\nTasks (%d):\n", len(parsed.Tasks))
				for i, t := range parsed.Tasks {
					fmt.Fprintf(cmd.OutOrStdout(), "  %d. [%s] %s\n", i+1, t.Schedule, t.Command)
				}
			}

			return nil
		},
	}
}
