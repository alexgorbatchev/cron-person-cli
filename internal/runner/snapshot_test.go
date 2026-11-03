package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunnerUsesTheSnapshotItAuthorized(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	st, dir := runnerProject(t, "APP_VALUE=approved\n@daily true\n")
	t.Cleanup(func() { authorized = func() {} })
	authorized = func() {
		if err := os.WriteFile(filepath.Join(dir, ".cronrc"), []byte("APP_VALUE=unapproved\n@daily true\n"), 0o600); err != nil {
			t.Error(err)
		}
	}
	var stdout bytes.Buffer
	if err := NewRunner(st).Exec(t.Context(), ExecOptions{Dir: dir, Command: `printf '%s' "$APP_VALUE"`, Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "approved" {
		t.Fatalf("task saw APP_VALUE=%q, want the authorized snapshot's value", stdout.String())
	}
}
