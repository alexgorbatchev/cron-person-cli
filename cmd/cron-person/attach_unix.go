//go:build unix

package main

import (
	"os"
	"syscall"
)

// recorderProcAttr starts the recorder in its own session, out of reach of the
// terminal's job-control signals and of the terminal closing.
func recorderProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// interruptRecorder asks the recorder to terminate the task; it records the
// outcome before exiting.
func interruptRecorder(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
