package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func TestRunner_Exec_Success(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "allowed.json")
	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tempDir, "my-app")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cronrcContent := `
CUSTOM_ENV_VAR=hello_world
@hourly pwd && echo $CUSTOM_ENV_VAR
`
	if err := os.WriteFile(filepath.Join(projectDir, ".cronrc"), []byte(cronrcContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Allow the directory
	if _, err := st.Allow(projectDir); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(st)

	var stdout, stderr bytes.Buffer
	execOpts := ExecOptions{
		Dir:     projectDir,
		Command: "pwd && echo \"ENV=$CUSTOM_ENV_VAR\"",
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	err = r.Exec(context.Background(), execOpts)
	if err != nil {
		t.Fatalf("unexpected exec error: %v", err)
	}

	out := stdout.String()
	// Should have executed with cwd = projectDir
	if !strings.Contains(out, projectDir) {
		t.Errorf("expected output to contain cwd %q, got: %s", projectDir, out)
	}
	if !strings.Contains(out, "ENV=hello_world") {
		t.Errorf("expected output to contain ENV=hello_world, got: %s", out)
	}
}

func TestRunner_Exec_Unauthorized(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "allowed.json")
	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tempDir, "unauthorized-app")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cronrcContent := `@hourly echo test`
	if err := os.WriteFile(filepath.Join(projectDir, ".cronrc"), []byte(cronrcContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Do NOT allow
	r := NewRunner(st)
	var stdout, stderr bytes.Buffer
	err = r.Exec(context.Background(), ExecOptions{
		Dir:     projectDir,
		Command: "echo test",
		Stdout:  &stdout,
		Stderr:  &stderr,
	})

	if err == nil {
		t.Fatal("expected error executing in unauthorized directory, got nil")
	}
	if !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("expected unauthorized error, got: %v", err)
	}
}

func TestRunner_Exec_HashChanged(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "allowed.json")
	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tempDir, "tampered-app")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cronrcPath := filepath.Join(projectDir, ".cronrc")
	_ = os.WriteFile(cronrcPath, []byte("@hourly echo 1"), 0o644)
	_, _ = st.Allow(projectDir)

	// Modify without re-allow
	_ = os.WriteFile(cronrcPath, []byte("@hourly echo 2"), 0o644)

	r := NewRunner(st)
	var stdout, stderr bytes.Buffer
	err = r.Exec(context.Background(), ExecOptions{
		Dir:     projectDir,
		Command: "echo 2",
		Stdout:  &stdout,
		Stderr:  &stderr,
	})

	if err == nil {
		t.Fatal("expected error executing tampered directory, got nil")
	}
	if !strings.Contains(err.Error(), "changed") && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected changed/blocked error, got: %v", err)
	}
}
