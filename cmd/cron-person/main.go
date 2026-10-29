package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

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
		return exitCode(err)
	}
	return 0
}

// exitCode passes a task's own exit status through to cron and callers, using
// the shell convention of 128+N for termination by signal N. Every other
// failure exits 1.
func exitCode(err error) int {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 1
	}
	if code := exitErr.ExitCode(); code > 0 {
		return code
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return 1
}
