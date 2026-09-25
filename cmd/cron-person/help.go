package main

import (
	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/spf13/cobra"
)

var techCatalog = cobrahelptree.TechCatalog{
	"cron-person": {
		Summary:     "Higher-order per-directory crontab manager with direnv-style shell integration",
		Description: "Higher-order per-directory crontab manager that executes tasks in their respective directory contexts with direnv-style allow/deny authorization.",
	},
	"cron-person allow": {
		Summary:     "Authorize .cronrc in directory",
		Description: "Computes SHA-256 hash and records directory as authorized to execute.",
		Args: []cobrahelptree.ArgSpec{
			{Name: "[path]", Description: "Target directory containing .cronrc (defaults to current directory or nearest ancestor)"},
		},
		MutatesDB: true,
	},
	"cron-person deny": {
		Summary:     "Revoke authorization for directory",
		Description: "Removes directory from authorization store to prevent task execution.",
		Args: []cobrahelptree.ArgSpec{
			{Name: "[path]", Description: "Target directory containing .cronrc (defaults to current directory or nearest ancestor)"},
		},
		MutatesDB: true,
	},
	"cron-person status": {
		Summary:     "Display overall system status and metrics",
		Description: "Outputs active directories, allowed count, and total scheduled tasks.",
	},
	"cron-person dir": {
		Summary:     "Inspect and manage directory authorizations and .cronrc configurations",
		Description: "Commands for managing authorized directories and inspecting their .cronrc configurations.",
	},
	"cron-person dir allow": {
		Summary:     "Authorize .cronrc in the specified directory",
		Description: "Records SHA-256 digest of .cronrc to authorize it for automated execution.",
		Args: []cobrahelptree.ArgSpec{
			{Name: "[path]", Description: "Directory containing .cronrc (defaults to current directory or nearest ancestor)"},
		},
		MutatesDB: true,
	},
	"cron-person dir deny": {
		Summary:     "Revoke authorization for the specified directory",
		Description: "Revokes authorization for the specified directory, blocking cron runs.",
		Args: []cobrahelptree.ArgSpec{
			{Name: "[path]", Description: "Directory containing .cronrc (defaults to current directory or nearest ancestor)"},
		},
		MutatesDB: true,
	},
	"cron-person dir list": {
		Summary:     "List all tracked directories and their authorization status",
		Description: "Displays all known directories with current authorization health (allowed, changed, missing).",
	},
	"cron-person dir check": {
		Summary:     "Validate syntax and parseability of a .cronrc file",
		Description: "Parses .cronrc in the given directory and prints detected schedule lines and environment variables.",
		Args: []cobrahelptree.ArgSpec{
			{Name: "[path]", Description: "Directory containing .cronrc to check (defaults to current directory)"},
		},
	},
	"cron-person dir status": {
		Summary:     "Show status and tasks for a specific directory",
		Description: "Inspects authorization status, recorded hash, and task list for a directory.",
		Args: []cobrahelptree.ArgSpec{
			{Name: "[path]", Description: "Directory to inspect (defaults to current directory)"},
		},
	},
	"cron-person crontab": {
		Summary:     "Synchronize and inspect system crontab integration",
		Description: "Commands to generate, inspect, install, and remove managed user crontab blocks.",
	},
	"cron-person crontab sync": {
		Summary:     "Compile authorized directory jobs and install into user crontab",
		Description: "Compiles all authorized tasks into a managed block and updates system crontab via crontab(1).",
		MutatesDB:   true,
	},
	"cron-person crontab show": {
		Summary:     "Preview generated crontab block without modifying crontab",
		Description: "Prints the formatted crontab block that would be written to the system crontab.",
	},
	"cron-person crontab uninstall": {
		Summary:     "Remove cron-person managed block from system crontab",
		Description: "Strips the managed block from the active user crontab while keeping other user jobs intact.",
		MutatesDB:   true,
	},
	"cron-person task": {
		Summary:     "Inspect and trigger scheduled tasks",
		Description: "Commands for listing and manually triggering individual tasks.",
	},
	"cron-person task list": {
		Summary:     "List all tasks defined across all authorized directories",
		Description: "Displays all active tasks grouped by directory with their cron schedules.",
	},
	"cron-person task run": {
		Summary:     "Manually execute a task inside a target directory",
		Description: "Executes the specified command inside the target directory with .cronrc environment variables loaded.",
		Args: []cobrahelptree.ArgSpec{
			{Name: "<dir>", Description: "Directory path containing .cronrc"},
			{Name: "<command...>", Description: "Shell command string to execute"},
		},
	},
	"cron-person exec": {
		Summary:     "Execute a command in an authorized directory with cwd and environment",
		Description: "Underlying execution engine invoked by crontab runner: verifies authorization, sets cwd, and runs command.",
		Args: []cobrahelptree.ArgSpec{
			{Name: "<dir>", Description: "Target directory path"},
			{Name: "<command...>", Description: "Shell command to execute in directory"},
		},
	},
	"cron-person hook": {
		Summary:     "Generate shell integration hook script",
		Description: "Prints hook script for bash, zsh, or fish for direnv-like prompt warnings on unauthorized .cronrc files.",
		Args: []cobrahelptree.ArgSpec{
			{Name: "<bash|zsh|fish>", Description: "Target shell name"},
		},
	},
}

func setupHelp(cmd *cobra.Command) {
	_ = cobrahelptree.SetupWithOptions(cmd, cobrahelptree.HelpOptions{
		Catalog: techCatalog,
		Tree: cobrahelptree.TreeOptions{
			HideGeneratedCommands: true,
		},
	})
}
