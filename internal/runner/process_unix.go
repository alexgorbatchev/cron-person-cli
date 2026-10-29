//go:build unix

package runner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateGroup(p *os.Process) error {
	return signalGroup(p, syscall.SIGTERM)
}

func killGroup(p *os.Process) error {
	return signalGroup(p, syscall.SIGKILL)
}

// signalGroup signals every process in the group p leads. A group that is
// already gone reports os.ErrProcessDone, which os/exec treats as nothing left
// to cancel.
func signalGroup(p *os.Process, sig syscall.Signal) error {
	err := syscall.Kill(-p.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
