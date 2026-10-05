package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cliCrontab(t *testing.T) string {
	t.Helper()
	fixture, err := filepath.Abs("../../testdata")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "crontab")
	t.Setenv("PATH", fixture)
	t.Setenv("TEST_CRONTAB_FILE", path)
	t.Setenv("TEST_CRONTAB_READ_ERROR", "")
	t.Setenv("TEST_CRONTAB_WRITE_ERROR", "")
	return path
}

func TestCLICrontabBothModes(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run("agent="+mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			path := cliCrontab(t)
			dir, state := cliProject(t)
			userJobs := "@weekly personal-job\n"
			writeCLIFile(t, path, userJobs)
			requireCLIOutput(t, []string{"--store", state, "allow", dir})
			requireCLIOutput(t, []string{"--store", state, "crontab", "sync"}, modeText(mode, "Synchronized 1", "synced_directories: 1"))
			data, err := os.ReadFile(path)
			if err != nil || !strings.HasPrefix(string(data), userJobs) || !strings.Contains(string(data), "printf scheduled") {
				t.Fatalf("synchronized crontab=%q error=%v", data, err)
			}
			requireCLIOutput(t, []string{"crontab", "uninstall"}, modeText(mode, "Removed cron-person", "crontab: uninstalled"))
			data, err = os.ReadFile(path)
			if err != nil || !strings.HasPrefix(string(data), userJobs) || strings.Contains(string(data), "printf scheduled") {
				t.Fatalf("uninstalled crontab=%q error=%v", data, err)
			}
		})
	}
}

func TestCLICrontabFailures(t *testing.T) {
	for _, verb := range []string{"sync", "uninstall"} {
		for _, failure := range []string{"read", "write"} {
			t.Run(verb+"/"+failure, func(t *testing.T) {
				path := cliCrontab(t)
				writeCLIFile(t, path, "@daily preserved\n")
				dir, state := cliProject(t)
				requireCLIOutput(t, []string{"--store", state, "allow", dir})
				if failure == "read" {
					t.Setenv("TEST_CRONTAB_READ_ERROR", "read refused")
				} else {
					t.Setenv("TEST_CRONTAB_WRITE_ERROR", "write refused")
				}
				out, err := executeCommand("--store", state, "crontab", verb)
				if err == nil || !strings.Contains(err.Error(), failure+" refused") {
					t.Fatalf("command lost crontab error: %v, output=%s", err, out)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "@daily preserved\n" {
					t.Fatalf("command failure changed crontab: %q, %v", data, err)
				}
			})
		}
	}
}
