//go:build unix

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

func TestAttachedRunForwardsInputAndReportsExitStatus(t *testing.T) {
	f := startAttached(t, `read line; echo "got $line"; echo oops >&2; cat; echo "end of input"; exit 3`, true)
	if _, err := io.WriteString(f.stdin, "hello\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.stdin.Close(); err != nil {
		t.Fatal(err)
	}
	var status exitStatus
	if err := f.wait(t); !errors.As(err, &status) || status != 3 {
		t.Fatalf("result = %v, want exit status 3", err)
	}
	if f.stdout.String() != "got hello\nend of input\n" || !strings.Contains(f.stderr.String(), "oops\n") {
		t.Fatalf("stdout %q stderr %q", f.stdout.String(), f.stderr.String())
	}
	if run := f.finalRun(t); run.Trigger != history.TriggerManual || *run.ExitCode != 3 {
		t.Fatalf("run = %+v", run)
	}
}

func TestAttachedRunCtrlCPrompt(t *testing.T) {
	const command = "echo ready; sleep 1; echo finished"
	for _, tt := range []struct {
		name    string
		respond func(f *attachFixture)
		detach  bool
	}{
		{"yes terminates", func(f *attachFixture) { _, _ = io.WriteString(f.stdin, "y\n") }, false},
		{"second interrupt terminates", func(f *attachFixture) { f.signals <- os.Interrupt }, false},
		{"no detaches", func(f *attachFixture) { _, _ = io.WriteString(f.stdin, "n\n") }, true},
		{"empty answer detaches", func(f *attachFixture) { _, _ = io.WriteString(f.stdin, "\n") }, true},
		{"end of input detaches", func(f *attachFixture) { _ = f.stdin.Close() }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := startAttached(t, command, true)
			f.waitForRunning(t)
			f.signals <- os.Interrupt
			eventually(t, "the prompt", func() bool { return strings.Contains(f.stderr.String(), terminatePrompt) })
			tt.respond(f)
			err := f.wait(t)
			run := f.finalRun(t)
			out := &bytes.Buffer{}
			if err := history.New(history.DirForStore(f.state)).WriteOutput(out, run, 0); err != nil {
				t.Fatal(err)
			}
			if tt.detach {
				if err != nil || !strings.Contains(f.stderr.String(), "keeps running in the background as run "+run.ID) {
					t.Fatalf("detach result %v, stderr %q", err, f.stderr.String())
				}
				// The recorder keeps capturing after the foreground has gone.
				if run.Status != history.StatusSucceeded || !strings.Contains(out.String(), "[out] finished") || strings.Contains(f.stdout.String(), "finished") {
					t.Fatalf("detached run = %+v, log %q, foreground stdout %q", run, out.String(), f.stdout.String())
				}
				return
			}
			var status exitStatus
			if !errors.As(err, &status) || status != 128+exitStatus(syscall.SIGTERM) {
				t.Fatalf("terminate result = %v", err)
			}
			if run.Status != history.StatusFailed || !strings.Contains(run.Error, "signal: terminated") || strings.Contains(out.String(), "finished") {
				t.Fatalf("terminated run = %+v, log %q", run, out.String())
			}
		})
	}
}

func TestAttachedRunTerminatesWithoutPrompting(t *testing.T) {
	for _, tt := range []struct {
		name        string
		interactive bool
		sig         os.Signal
	}{
		{"interrupt without a terminal", false, os.Interrupt},
		{"terminate signal", true, syscall.SIGTERM},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := startAttached(t, "echo ready; sleep 30", tt.interactive)
			f.waitForRunning(t)
			f.signals <- tt.sig
			var status exitStatus
			if err := f.wait(t); !errors.As(err, &status) || status != 128+exitStatus(syscall.SIGTERM) {
				t.Fatalf("result = %v", err)
			}
			if strings.Contains(f.stderr.String(), terminatePrompt) {
				t.Fatalf("prompted: %q", f.stderr.String())
			}
		})
	}
}

func TestAttachedRunReportsStartFailure(t *testing.T) {
	a := attachedRun{stdin: strings.NewReader(""), stdout: io.Discard, stderr: io.Discard}
	if err := a.run(exec.Command(filepath.Join(t.TempDir(), "missing"))); err == nil || !strings.Contains(err.Error(), "starting task recorder") {
		t.Fatalf("run error = %v", err)
	}
}

func TestRecorderResult(t *testing.T) {
	if err := recorderResult(nil); err != nil {
		t.Fatal(err)
	}
	if err := recorderResult(errors.New("wait failed")); err == nil || !strings.Contains(err.Error(), "task recorder failed") {
		t.Fatalf("recorderResult = %v", err)
	}
	killed := exec.Command("/bin/sh", "-c", "kill -9 $$").Run()
	if err := recorderResult(killed); err == nil || !strings.Contains(err.Error(), "task recorder failed") {
		t.Fatalf("recorderResult(killed) = %v", err)
	}
}

func TestIsYes(t *testing.T) {
	for answer, want := range map[string]bool{"y\n": true, " YES \n": true, "n\n": false, "\n": false, "yep": false} {
		if isYes([]byte(answer)) != want {
			t.Fatalf("isYes(%q) != %v", answer, want)
		}
	}
	if isTerminal(strings.NewReader("")) {
		t.Fatal("a string reader is not a terminal")
	}
}
