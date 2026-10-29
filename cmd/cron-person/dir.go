package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v3"
	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/display"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
	"github.com/spf13/cobra"
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
				return writeOutput(cmd, fmt.Sprintf("dir: %s\nstatus: allowed\nhash: %s\n", display.Path(rec.Dir), rec.Hash))
			}

			return writeOutput(cmd, fmt.Sprintf("[OK] Authorized .cronrc in %s\n     Hash: %s\n", display.Path(rec.Dir), rec.Hash))
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
				return writeOutput(cmd, fmt.Sprintf("dir: %s\nstatus: denied\n", display.Path(dir)))
			}

			return writeOutput(cmd, fmt.Sprintf("[OK] Revoked authorization for %s\n", display.Path(dir)))
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

			records, err := st.List()
			if err != nil {
				return err
			}

			var out strings.Builder
			if cobrahelptree.IsAgentMode() {
				for _, rec := range records {
					status, _, err := st.Status(rec.Dir)
					if err != nil {
						return fmt.Errorf("checking authorization for %s: %w", rec.Dir, err)
					}
					fmt.Fprintf(&out, "dir: %s\tstatus: %s\thash: %s\n", display.Path(rec.Dir), status, rec.Hash)
				}
				return writeOutput(cmd, out.String())
			}

			if len(records) == 0 {
				return writeOutput(cmd, "No tracked directories. Use 'cron-person allow <path>' to authorize a directory.\n")
			}

			fmt.Fprintln(&out, "Tracked Directories:")
			for _, rec := range records {
				status, _, err := st.Status(rec.Dir)
				if err != nil {
					return fmt.Errorf("checking authorization for %s: %w", rec.Dir, err)
				}
				statusTag := "[OK]"
				if status != store.StatusAllowed {
					statusTag = "[WARN]"
				}
				fmt.Fprintf(&out, "  %s %s (%s)\n", statusTag, display.Path(rec.Dir), status)
				fmt.Fprintf(&out, "     Hash: %s\n", rec.Hash)
			}
			return writeOutput(cmd, out.String())
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
				return writeOutput(cmd, fmt.Sprintf("path: %s\ntasks_count: %d\nhash: %s\n", display.Path(parsed.Path), len(parsed.Tasks), parsed.Hash))
			}

			var out strings.Builder
			fmt.Fprintf(&out, "[OK] Syntax valid for %s\n", display.Path(parsed.Path))
			fmt.Fprintf(&out, "     Found %d scheduled task(s)\n", len(parsed.Tasks))
			if len(parsed.Env) > 0 {
				fmt.Fprintf(&out, "     Environment variables: %d defined\n", len(parsed.Env))
			}
			for i, t := range parsed.Tasks {
				fmt.Fprintf(&out, "     Task #%d (line %d): %s => %s\n", i+1, t.LineNumber, t.Schedule, t.Command)
			}
			return writeOutput(cmd, out.String())
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

			status, rec, snapshot, err := st.Inspect(dir)
			if err != nil {
				return err
			}
			var parsed *cronrc.File
			if snapshot != nil {
				path := filepath.Join(dir, ".cronrc")
				parsed, err = cronrc.Parse(bytes.NewReader(snapshot), path)
				if err != nil {
					return fmt.Errorf("parsing %s: %w", path, err)
				}
			}

			var out strings.Builder
			if cobrahelptree.IsAgentMode() {
				fmt.Fprintf(&out, "dir: %s\nstatus: %s\n", display.Path(dir), status)
				if rec != nil {
					fmt.Fprintf(&out, "hash: %s\n", rec.Hash)
				}
				return writeOutput(cmd, out.String())
			}

			fmt.Fprintf(&out, "Directory: %s\n", display.Path(dir))
			fmt.Fprintf(&out, "Status:    %s\n", status)
			if rec != nil {
				fmt.Fprintf(&out, "Hash:      %s\n", rec.Hash)
				fmt.Fprintf(&out, "Allowed:   %s\n", rec.AllowedAt.Format("2006-01-02 15:04:05 UTC"))
			}

			if parsed != nil {
				fmt.Fprintf(&out, "\nTasks (%d):\n", len(parsed.Tasks))
				for i, t := range parsed.Tasks {
					fmt.Fprintf(&out, "  %d. [%s] %s\n", i+1, t.Schedule, t.Command)
				}
			}

			return writeOutput(cmd, out.String())
		},
	}
}
