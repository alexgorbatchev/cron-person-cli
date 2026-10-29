package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configProject(t *testing.T) (state, project string) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	tmp := t.TempDir()
	state = filepath.Join(tmp, "state", "allowed.json")
	project = filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".cronrc"), []byte("@hourly true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := executeCommand("--store", state, "allow", project); err != nil {
		t.Fatalf("allow: %v %s", err, out)
	}
	return state, project
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLI_ConfiguredRetentionPrunesHistory(t *testing.T) {
	t.Setenv("AGENT", "1")
	state, project := configProject(t)
	cfg := writeConfig(t, `{"history":{"retention_days":0,"max_size_mb":1}}`)
	// Each run captures about 700 KB, so only the newest fits in 1 MB.
	for range 3 {
		if out, err := executeCommand("--store", state, "--config", cfg, "exec", project, "--", "head -c 700000 /dev/zero | tr '\\0' x"); err != nil {
			t.Fatalf("exec: %v %.200s", err, out)
		}
	}
	out, err := executeCommand("--store", state, "--config", cfg, "task", "history")
	if err != nil || strings.Count(out, "\n") != 1 {
		t.Fatalf("history after pruning = %.300q, %v", out, err)
	}

	unlimited := writeConfig(t, `{"history":{"retention_days":0,"max_size_mb":0}}`)
	for range 2 {
		if _, err := executeCommand("--store", state, "--config", unlimited, "exec", project, "--", "true"); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := executeCommand("--store", state, "task", "history"); err != nil || strings.Count(out, "\n") != 3 {
		t.Fatalf("history without limits = %q, %v", out, err)
	}
}

func TestCLI_InvalidConfigUsesDefaultsQuietlyForCron(t *testing.T) {
	t.Setenv("AGENT", "0")
	state, project := configProject(t)
	cfg := writeConfig(t, `{"history":{"max_size_mb":-5}}`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--store", state, "--config", cfg, "exec", project, "--", "echo ran; exit 4"}, &stdout, &stderr); code != 4 || stdout.String() != "ran\n" || strings.Contains(stderr.String(), "max_size_mb") {
		t.Fatalf("cron run: exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--store", state, "--config", cfg, "task", "run", project, "echo manual"}, &stdout, &stderr); code != 0 || !strings.Contains(stderr.String(), "[WARN] using default settings: invalid config") {
		t.Fatalf("manual run: exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}

	t.Setenv("AGENT", "1")
	out, err := executeCommand("--store", state, "task", "history")
	if err != nil || strings.Count(out, "warnings: 1") != 2 {
		t.Fatalf("history = %q, %v", out, err)
	}
	id := agentField(strings.Split(out, "\n")[1], "id")
	if out, err := executeCommand("--store", state, "task", "log", id); err != nil || !strings.Contains(out, "warning: using default settings: invalid config") {
		t.Fatalf("log = %q, %v", out, err)
	}
	t.Setenv("AGENT", "0")
	if out, err := executeCommand("--store", state, "task", "history"); err != nil || !strings.Contains(out, "Warnings: 1 (see task log)") {
		t.Fatalf("human history = %q, %v", out, err)
	}
}

func TestCLI_ConfigProblemsSurfaceInStatusAndSync(t *testing.T) {
	t.Setenv("AGENT", "0")
	state, _ := configProject(t)
	missing := filepath.Join(t.TempDir(), "absent.json")
	valid := writeConfig(t, `{"history":{"retention_days":3}}`)
	invalid := writeConfig(t, `{"history":{"retention_days":"soon"}}`)
	for _, tt := range []struct {
		path, want string
	}{
		{missing, "(defaults)"},
		{valid, "(loaded)"},
		{invalid, "Configuration Error:  parsing config"},
	} {
		out, err := executeCommand("--store", state, "--config", tt.path, "status")
		if err != nil || !strings.Contains(out, tt.want) {
			t.Fatalf("status with %s = %q, %v", tt.path, out, err)
		}
	}
	t.Setenv("AGENT", "1")
	if out, err := executeCommand("--store", state, "--config", invalid, "status"); err != nil || !strings.Contains(out, "config_status: invalid\nconfig_error: parsing config") {
		t.Fatalf("agent status = %q, %v", out, err)
	}
	if _, err := executeCommand("--store", state, "--config", invalid, "crontab", "sync"); err == nil || !strings.Contains(err.Error(), "fix the configuration before syncing") {
		t.Fatalf("sync with invalid config = %v", err)
	}
}

func TestCLI_PerRunOutputLimit(t *testing.T) {
	t.Setenv("AGENT", "0")
	state, project := configProject(t)
	cfg := writeConfig(t, `{"history":{"max_run_mb":1}}`)
	var stdout bytes.Buffer
	if code := run([]string{"--store", state, "--config", cfg, "exec", project, "--", "head -c 2000000 /dev/zero | tr '\\0' x; echo; echo after"}, &stdout, io.Discard); code != 0 || !strings.HasSuffix(stdout.String(), "after\n") {
		t.Fatalf("exit %d, stdout ends %q", code, stdout.String()[max(0, stdout.Len()-20):])
	}
	out, err := executeCommand("--store", state, "task", "history")
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Fields(strings.Split(out, "\n")[1])[1]
	log, err := executeCommand("--store", state, "task", "log", id, "--tail", "1")
	if err != nil || !strings.Contains(log, "Truncated:  output beyond history.max_run_mb was not stored") || !strings.HasSuffix(log, "[cron-person] output truncated after 1048576 bytes\n") {
		t.Fatalf("log = %q, %v", log[max(0, len(log)-300):], err)
	}
}

func TestCLI_CrontabEmbedsConfigPath(t *testing.T) {
	state, _ := configProject(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	custom := filepath.Join(t.TempDir(), "custom.json")
	for _, tt := range []struct {
		args []string
		want string
	}{
		{nil, filepath.Join(xdg, "cron-person", "config.json")},
		{[]string{"--config", custom}, custom},
	} {
		out, err := executeCommand(append(append([]string{"--store", state}, tt.args...), "crontab", "show")...)
		if err != nil || !strings.Contains(out, "--config '"+tt.want+"' exec ") {
			t.Fatalf("crontab show %v = %q, %v; want config %s", tt.args, out, err, tt.want)
		}
	}
}
