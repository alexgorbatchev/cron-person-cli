package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v3"
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
		var status exitStatus
		if errors.As(err, &status) {
			// The failure was already reported by the process that produced it.
			return int(status)
		}
		if cobrahelptree.IsAgentMode() {
			fmt.Fprintf(stderr, "ERR: %s\n", display.Error(err))
		} else {
			fmt.Fprintf(stderr, "[ERROR] %s\n", display.Error(err))
		}
		return exitCode(err)
	}
	return 0
}

// exitStatus ends the process with a status that a child process has already
// explained on the relayed stderr.
type exitStatus int

func (s exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(s)) }

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
