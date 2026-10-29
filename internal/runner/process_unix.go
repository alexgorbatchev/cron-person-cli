//go:build unix

package runner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// taskGroup is the process group a task leads, which every process it starts
// joins unless it deliberately leaves.
type taskGroup struct {
	pid int
}

func prepareGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachGroup(cmd *exec.Cmd) (*taskGroup, error) {
	return &taskGroup{pid: cmd.Process.Pid}, nil
}

func (g *taskGroup) terminate() error { return g.signal(syscall.SIGTERM) }

func (g *taskGroup) kill() error { return g.signal(syscall.SIGKILL) }

func (g *taskGroup) close() error { return nil }

// signal signals every process in the group. A group that is already gone
// reports os.ErrProcessDone, which os/exec treats as nothing left to cancel.
func (g *taskGroup) signal(sig syscall.Signal) error {
	err := syscall.Kill(-g.pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
