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
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

// syncBuffer is a bytes.Buffer safe for the relay goroutines and the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type attachFixture struct {
	state, project string
	stdout, stderr *syncBuffer
	stdin          *io.PipeWriter
	signals        chan os.Signal
	result         chan error
}

// startAttached runs command through a foreground recorder, as `task run` does,
// with input from a pipe the test writes to and signals the test sends.
func startAttached(t *testing.T, command string, interactive bool) *attachFixture {
	t.Helper()
	state, project := configProject(t)
	inR, inW := io.Pipe()
	t.Cleanup(func() { _ = inW.Close() })
	f := &attachFixture{state: state, project: project, stdout: &syncBuffer{}, stderr: &syncBuffer{}, stdin: inW,
		signals: make(chan os.Signal, 2), result: make(chan error, 1)}
	idPath := filepath.Join(t.TempDir(), "run.id")
	a := attachedRun{stdin: inR, stdout: f.stdout, stderr: f.stderr, interactive: interactive, signals: f.signals, idPath: idPath}
	args := []string{"--store", state, "--config", filepath.Join(t.TempDir(), "config.json"), "exec",
		"--trigger", "manual", "--run-id-file", idPath, project, "--", command}
	go func() { f.result <- a.run(exec.Command(os.Args[0], args...)) }()
	return f
}

func (f *attachFixture) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-f.result:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("foreground run never returned")
		return nil
	}
}

func (f *attachFixture) runs(t *testing.T) []history.Run {
	t.Helper()
	runs, err := history.New(history.DirForStore(f.state)).List(history.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func (f *attachFixture) waitForRunning(t *testing.T) {
	t.Helper()
	eventually(t, "the run to start", func() bool {
		runs := f.runs(t)
		return len(runs) == 1 && runs[0].Status == history.StatusRunning && strings.Contains(f.stdout.String(), "ready")
	})
}

func (f *attachFixture) finalRun(t *testing.T) history.Run {
	t.Helper()
	var run history.Run
	eventually(t, "the run to finish", func() bool {
		runs := f.runs(t)
		if len(runs) == 1 && runs[0].Status != history.StatusRunning {
			run = runs[0]
			return true
		}
		return false
	})
	return run
}

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
