package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPropagatesTaskExitStatus(t *testing.T) {
	t.Setenv("AGENT", "0")
	t.Setenv("SHELL", "/bin/sh")
	tmp := t.TempDir()
	state := filepath.Join(tmp, "state", "allowed.json")
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".cronrc"), []byte("@hourly true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--store", state, "allow", project}, &stdout, &stderr); code != 0 {
		t.Fatalf("allow exited %d: %s", code, stderr.String())
	}
	for _, tt := range []struct {
		name    string
		args    []string
		want    int
		wantErr string
	}{
		{"success", []string{"exec", project, "--", "true"}, 0, ""},
		{"task exit status", []string{"exec", project, "--", "exit 7"}, 7, "exit status 7"},
		{"manual run exit status", []string{"task", "run", project, "exit 9"}, 9, "exit status 9"},
		{"signal", []string{"exec", project, "--", "kill -TERM $$"}, 128 + 15, "signal: terminated"},
		{"refused run", []string{"exec", filepath.Join(tmp, "unknown"), "--", "true"}, 1, "no .cronrc file found"},
		{"usage error", []string{"exec"}, 1, "requires at least 2 arg"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			code := run(append([]string{"--store", state}, tt.args...), &stdout, &stderr)
			if code != tt.want || !strings.Contains(stderr.String(), tt.wantErr) {
				t.Fatalf("exit %d, stderr %q; want %d containing %q", code, stderr.String(), tt.want, tt.wantErr)
			}
		})
	}
}
