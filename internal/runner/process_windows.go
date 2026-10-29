//go:build windows

package runner

import (
	"os"
	"os/exec"
)

// Windows has no POSIX process groups; cancellation ends the task's shell.
func setProcessGroup(*exec.Cmd) {}

func terminateGroup(p *os.Process) error { return p.Kill() }

func killGroup(p *os.Process) error { return p.Kill() }
