package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runProcess runs this test binary as cron-person in its own process (TestMain
// exports the switch), so exit codes and stream ownership are observed the
// way a shell or cron observes them. A nil stdout is captured.
func runProcess(t *testing.T, stdout *os.File, args ...string) (code int, out, errOut string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "AGENT=0", "SHELL=/bin/sh")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	if stdout != nil {
		cmd.Stdout = stdout
	}
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
		if code < 0 {
			t.Fatalf("%v: killed by a signal: %v", args, err)
		}
	case err != nil:
		t.Fatalf("%v: %v", args, err)
	}
	return code, outBuf.String(), errBuf.String()
}

func TestProcessExitCodesAndStreams(t *testing.T) {
	project, state := cliProject(t)
	if code, _, stderr := runProcess(t, nil, "--store", state, "allow", project); code != 0 {
		t.Fatalf("allow exited %d: %s", code, stderr)
	}
	empty := t.TempDir()
	for _, tt := range []struct {
		name       string
		args       []string
		code       int
		stdout     string
		stderrLine string
	}{
		{"success", []string{"--store", state, "status"}, 0, "Tracked Directories:  1", ""},
		{"argument error", []string{"dir", "status", "a", "b"}, 1, "", "[ERROR] accepts at most 1 arg(s), received 2"},
		{"operational error", []string{"--store", state, "dir", "check", empty}, 1, "", "[ERROR] invalid .cronrc"},
		{"task exit status", []string{"--store", state, "exec", project, "--", "exit 7"}, 7, "", "[ERROR] task failed in"},
		{"task killed by a signal", []string{"--store", state, "exec", project, "--", "kill -TERM $$"}, 128 + 15, "", "signal: terminated"},
		{"refused task", []string{"--store", state, "exec", empty, "--", "true"}, 1, "", "[ERROR] no .cronrc file found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runProcess(t, nil, tt.args...)
			if code != tt.code {
				t.Fatalf("exit %d, want %d; stderr %q", code, tt.code, stderr)
			}
			if tt.stdout == "" && stdout != "" || !strings.Contains(stdout, tt.stdout) {
				t.Fatalf("stdout %q, want %q", stdout, tt.stdout)
			}
			lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
			if tt.stderrLine == "" && stderr != "" || tt.stderrLine != "" && (len(lines) != 1 || !strings.Contains(lines[0], tt.stderrLine)) {
				t.Fatalf("stderr %q, want one line containing %q", stderr, tt.stderrLine)
			}
		})
	}
}

// Output that cannot be written must fail the command rather than report
// success with nothing printed.
func TestProcessReportsUnwritableStdout(t *testing.T) {
	_, state := cliProject(t)
	path := filepath.Join(t.TempDir(), "stdout")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	readOnly, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	code, _, stderr := runProcess(t, readOnly, "--store", state, "status")
	if code != 1 || !strings.HasPrefix(stderr, "[ERROR] ") || strings.Count(stderr, "\n") != 1 {
		t.Fatalf("exit %d, stderr %q; want exit 1 with one error line", code, stderr)
	}
}
