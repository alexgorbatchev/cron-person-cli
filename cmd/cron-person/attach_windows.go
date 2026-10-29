//go:build windows

package main

import (
	"os"
	"syscall"
)

const ctrlBreakEvent = 1

var procGenerateConsoleCtrlEvent = syscall.NewLazyDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")

// recorderProcAttr starts the recorder in its own process group, which the
// console's Ctrl+C does not reach.
func recorderProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// interruptRecorder sends Ctrl+Break to the recorder's process group, which Go
// delivers to it as os.Interrupt; it records the outcome before exiting.
func interruptRecorder(p *os.Process) error {
	if r1, _, err := procGenerateConsoleCtrlEvent.Call(ctrlBreakEvent, uintptr(p.Pid)); r1 == 0 {
		return err
	}
	return nil
}
