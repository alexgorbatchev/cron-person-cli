//go:build windows

package runner

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// taskGroup holds a task in a job object, which every process it starts joins,
// and in its own console process group for a graceful Ctrl+Break. A process the
// task starts in the instant before the job is assigned is not covered.
type taskGroup struct {
	pid uint32
	job windows.Handle
}

func prepareGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func attachGroup(cmd *exec.Cmd) (*taskGroup, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("creating job object: %w", err)
	}
	pid := uint32(cmd.Process.Pid)
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("opening task process: %w", err), windows.CloseHandle(job))
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return nil, errors.Join(fmt.Errorf("assigning task to job object: %w", err), windows.CloseHandle(job))
	}
	return &taskGroup{pid: pid, job: job}, nil
}

// terminate asks the task's process group to stop with Ctrl+Break. Without a
// console to deliver it, the job is ended at once instead.
func (g *taskGroup) terminate() error {
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, g.pid); err != nil {
		return g.kill()
	}
	return nil
}

func (g *taskGroup) kill() error { return windows.TerminateJobObject(g.job, 1) }

func (g *taskGroup) close() error { return windows.CloseHandle(g.job) }
