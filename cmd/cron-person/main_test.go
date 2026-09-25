package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executeCommand(args ...string) (string, error) {
	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestRootCommand_HelpAndVersion(t *testing.T) {
	out, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("expected no error for --help, got: %v", err)
	}
	if !strings.Contains(out, "cron-person") {
		t.Fatalf("expected help output to contain binary name, got: %s", out)
	}

	out, err = executeCommand("--version")
	if err != nil {
		t.Fatalf("expected no error for --version, got: %v", err)
	}
	if strings.TrimSpace(out) != version {
		t.Fatalf("expected version %q, got: %q", version, out)
	}
}

func TestCLI_Workflow_AllowDenyCheckList(t *testing.T) {
	tempDir := t.TempDir()
	storeFile := filepath.Join(tempDir, "state.json")
	projectDir := filepath.Join(tempDir, "sample-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cronrcPath := filepath.Join(projectDir, ".cronrc")
	cronrcContent := `
PROJECT_ENV=staging
@hourly ./sync.sh
0 2 * * * ./backup.sh
`
	if err := os.WriteFile(cronrcPath, []byte(cronrcContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Check syntax
	out, err := executeCommand("--store", storeFile, "dir", "check", projectDir)
	if err != nil {
		t.Fatalf("check failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "Found 2 scheduled task(s)") {
		t.Errorf("expected 2 tasks reported, got: %s", out)
	}

	// 2. Allow directory
	out, err = executeCommand("--store", storeFile, "allow", projectDir)
	if err != nil {
		t.Fatalf("allow failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "Authorized") {
		t.Errorf("expected Authorized output, got: %s", out)
	}

	// 3. Status check
	out, err = executeCommand("--store", storeFile, "status")
	if err != nil {
		t.Fatalf("status failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "Allowed Directories:  1") || !strings.Contains(out, "Active Tasks:         2") {
		t.Errorf("unexpected status output: %s", out)
	}

	// 4. List tasks
	out, err = executeCommand("--store", storeFile, "task", "list")
	if err != nil {
		t.Fatalf("task list failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "./sync.sh") || !strings.Contains(out, "./backup.sh") {
		t.Errorf("expected tasks in list, got: %s", out)
	}

	// 5. Preview crontab show
	out, err = executeCommand("--store", storeFile, "crontab", "show")
	if err != nil {
		t.Fatalf("crontab show failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "cron-person managed block") || !strings.Contains(out, projectDir) {
		t.Errorf("expected managed block with projectDir, got: %s", out)
	}

	// 6. Test exec
	out, err = executeCommand("--store", storeFile, "exec", projectDir, "--", "echo \"CWD=$(pwd)\"")
	if err != nil {
		t.Fatalf("exec failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "CWD="+projectDir) {
		t.Errorf("expected exec cwd to match %s, got: %s", projectDir, out)
	}

	// 7. Deny directory
	out, err = executeCommand("--store", storeFile, "deny", projectDir)
	if err != nil {
		t.Fatalf("deny failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "Revoked authorization") {
		t.Errorf("expected Revoked output, got: %s", out)
	}

	// 8. Exec should now fail
	out, err = executeCommand("--store", storeFile, "exec", projectDir, "--", "echo should-fail")
	if err == nil {
		t.Fatalf("expected exec to fail after deny, got success, out: %s", out)
	}
}

func TestCLI_HookGeneration(t *testing.T) {
	shells := []string{"bash", "zsh", "fish"}
	for _, shell := range shells {
		out, err := executeCommand("hook", shell)
		if err != nil {
			t.Fatalf("hook %s failed: %v", shell, err)
		}
		if !strings.Contains(out, "cron-person") {
			t.Errorf("hook %s missing binary name: %s", shell, out)
		}
	}
}

func TestCLI_AgentMode(t *testing.T) {
	t.Setenv("AGENT", "1")
	tempDir := t.TempDir()
	storeFile := filepath.Join(tempDir, "state.json")

	out, err := executeCommand("--store", storeFile, "status")
	if err != nil {
		t.Fatalf("status failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "tracked_dirs: 0") {
		t.Errorf("expected agent mode key-value output, got: %s", out)
	}
}
