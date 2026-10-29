//go:build windows

package main

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// recorderProcAttr starts the recorder in its own process group, which the
// console's Ctrl+C does not reach.
func recorderProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// interruptRecorder sends Ctrl+Break to the recorder's process group, which Go
// delivers to it as os.Interrupt; it records the outcome before exiting.
func interruptRecorder(p *os.Process) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.Pid))
}
