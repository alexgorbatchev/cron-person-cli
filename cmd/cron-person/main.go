package main

import (
	"fmt"
	"io"
	"os"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/alexgorbatchev/cron-person-cli/internal/display"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cmd := newRootCommand()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	if err := cmd.Execute(); err != nil {
		if cobrahelptree.IsAgentMode() {
			fmt.Fprintf(stderr, "ERR: %s\n", display.Error(err))
		} else {
			fmt.Fprintf(stderr, "[ERROR] %s\n", display.Error(err))
		}
		return 1
	}
	return 0
}
