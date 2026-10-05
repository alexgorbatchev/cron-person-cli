package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func cliProject(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	writeCLIFile(t, filepath.Join(dir, ".cronrc"), "CLI_VALUE=from-file\n@daily printf scheduled\n")
	return dir, filepath.Join(dir, "state.json")
}

func requireCLIOutput(t *testing.T, args []string, want ...string) string {
	t.Helper()
	out, err := executeCommand(args...)
	if err != nil {
		t.Fatalf("%v: %v, output=%s", args, err, out)
	}
	for _, part := range want {
		if !strings.Contains(out, part) {
			t.Fatalf("%v: output=%q, missing %q", args, out, part)
		}
	}
	return out
}

func modeText(mode, human, agent string) string {
	if mode == "1" {
		return agent
	}
	return human
}

func TestDirectoryLifecycleBothModes(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run("agent="+mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			dir, state := cliProject(t)
			child := filepath.Join(dir, "child")
			if err := os.Mkdir(child, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(child)
			run := func(want string, args ...string) string {
				return requireCLIOutput(t, append([]string{"--store", state}, args...), want)
			}
			run(modeText(mode, "No tracked directories.", ""), "dir", "list")
			run(modeText(mode, "No tasks found", ""), "task", "list")
			run(modeText(mode, "Status:    unauthorized", "status: unauthorized"), "dir", "status")
			run(modeText(mode, "Found 1 scheduled task(s)", "tasks_count: 1"), "dir", "check")
			run(modeText(mode, "Authorized", "status: allowed"), "dir", "allow")
			run(dir, "dir", "list")
			run(modeText(mode, "Status:    allowed", "status: allowed"), "dir", "status")
			run(modeText(mode, "Active Tasks:         1", "total_tasks: 1"), "status")
			run("printf scheduled", "task", "list")
			run("printf scheduled", "crontab", "show")
			writeCLIFile(t, filepath.Join(dir, ".cronrc"), "@hourly printf changed\n")
			run(modeText(mode, "[WARN]", "status: changed"), "dir", "list")
			run(modeText(mode, "Status:    changed", "status: changed"), "dir", "status")
			run(modeText(mode, "Active Tasks:         0", "total_tasks: 0"), "status")
			if out := run("", "task", "list"); strings.Contains(out, "printf changed") {
				t.Fatalf("changed schedule leaked into task list: %s", out)
			}
			if out := run("", "crontab", "show"); strings.Contains(out, "printf changed") {
				t.Fatalf("changed schedule leaked into crontab: %s", out)
			}
			run(modeText(mode, "Revoked authorization", "status: denied"), "dir", "deny")
			run(modeText(mode, "Status:    unauthorized", "status: unauthorized"), "dir", "status")
		})
	}
}

func TestDirectoryWithoutCronrcAndDefaultStore(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run("agent="+mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			dir := t.TempDir()
			t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
			t.Chdir(dir)
			requireCLIOutput(t, []string{"dir", "status"}, modeText(mode, "Status:    missing_cronrc", "status: missing_cronrc"))
			requireCLIOutput(t, []string{"deny"}, modeText(mode, "Revoked authorization", "status: denied"))
			writeCLIFile(t, filepath.Join(dir, ".cronrc"), "@hourly true\n")
			requireCLIOutput(t, []string{"allow"}, modeText(mode, "Authorized", "status: allowed"))
			if _, err := os.Stat(filepath.Join(dir, "state", "cron-person", "allowed.json")); err != nil {
				t.Fatalf("default state was not saved: %v", err)
			}
		})
	}
}

func TestCLIHookExportBothModes(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run("agent="+mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			dir, state := cliProject(t)
			t.Chdir(dir)
			args := []string{"--store", state, "hook", "export", "bash"}
			requireCLIOutput(t, args, "unauthorized .cronrc", modeText(mode, "[WARN]", "WARN:"))
			requireCLIOutput(t, []string{"--store", state, "allow"}, "")
			if out := requireCLIOutput(t, args); out != "" {
				t.Fatalf("allowed hook was not silent: %q", out)
			}
			writeCLIFile(t, filepath.Join(dir, ".cronrc"), "@daily changed\n")
			requireCLIOutput(t, args, "modified .cronrc")
		})
	}
}
