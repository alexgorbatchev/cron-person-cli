//go:build windows

package main

import (
	"errors"
	"syscall"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

func TestAttachedRunTerminatesTheRecorderOnWindows(t *testing.T) {
	f := startAttached(t, "echo ready; sleep 30", false)
	f.waitForRunning(t)
	f.signals <- syscall.SIGTERM
	var status exitStatus
	if err := f.wait(t); !errors.As(err, &status) {
		t.Fatalf("result = %v, stderr %q", err, f.stderr.String())
	}
	if run := f.finalRun(t); run.Status != history.StatusFailed {
		t.Fatalf("run = %+v", run)
	}
}
