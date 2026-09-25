package cronrc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_StandardCrontab(t *testing.T) {
	input := `
# Project level cron tasks
SHELL=/bin/bash
PROJECT_ENV=production

# Run backup every hour
0 * * * * ./scripts/backup.sh --full

# Clean temporary files every day at midnight
@daily make clean-tmp

# Every 15 minutes sync
*/15 * * * * sync-worker --queue=default

# Every 2 hours
@every 2h ./worker.sh

# At reboot
@reboot ./startup.sh

# Quoted environment variable
API_KEY="secret 123"
OTHER_KEY='hello world'
`

	file, err := Parse(strings.NewReader(input), "/tmp/project/.cronrc")
	if err != nil {
		t.Fatalf("unexpected error parsing cronrc: %v", err)
	}

	if file.Dir != "/tmp/project" {
		t.Errorf("expected dir /tmp/project, got %q", file.Dir)
	}

	if file.Env["SHELL"] != "/bin/bash" {
		t.Errorf("expected SHELL=/bin/bash, got %q", file.Env["SHELL"])
	}
	if file.Env["PROJECT_ENV"] != "production" {
		t.Errorf("expected PROJECT_ENV=production, got %q", file.Env["PROJECT_ENV"])
	}
	if file.Env["API_KEY"] != "secret 123" {
		t.Errorf("expected API_KEY='secret 123', got %q", file.Env["API_KEY"])
	}
	if file.Env["OTHER_KEY"] != "hello world" {
		t.Errorf("expected OTHER_KEY='hello world', got %q", file.Env["OTHER_KEY"])
	}

	if len(file.Tasks) != 5 {
		t.Fatalf("expected 5 tasks, got %d", len(file.Tasks))
	}

	if file.Tasks[0].Schedule != "0 * * * *" || file.Tasks[0].Command != "./scripts/backup.sh --full" {
		t.Errorf("task 0 mismatch: %+v", file.Tasks[0])
	}
	if file.Tasks[1].Schedule != "@daily" || file.Tasks[1].Command != "make clean-tmp" {
		t.Errorf("task 1 mismatch: %+v", file.Tasks[1])
	}
	if file.Tasks[2].Schedule != "*/15 * * * *" || file.Tasks[2].Command != "sync-worker --queue=default" {
		t.Errorf("task 2 mismatch: %+v", file.Tasks[2])
	}
	if file.Tasks[3].Schedule != "@every 2h" || file.Tasks[3].Command != "./worker.sh" {
		t.Errorf("task 3 mismatch: %+v", file.Tasks[3])
	}
	if file.Tasks[4].Schedule != "@reboot" || file.Tasks[4].Command != "./startup.sh" {
		t.Errorf("task 4 mismatch: %+v", file.Tasks[4])
	}
}

func TestParse_InvalidSchedule(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"invalid fields", "invalid schedule string here"},
		{"missing command for descriptor", "@daily"},
		{"missing duration for @every", "@every"},
		{"invalid cron format", "99 99 99 99 99 echo bad"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input), "/tmp/.cronrc")
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tt.input)
			}
		})
	}
}

func TestFindCronrc(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "a", "b", "c")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// No .cronrc yet
	found, err := FindCronrc(subDir)
	if err != nil {
		t.Fatal(err)
	}
	if found != "" {
		t.Fatalf("expected empty, got %q", found)
	}

	// Create .cronrc in a/
	cronrcPath := filepath.Join(tempDir, "a", ".cronrc")
	if err := os.WriteFile(cronrcPath, []byte("@daily echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	found, err = FindCronrc(subDir)
	if err != nil {
		t.Fatal(err)
	}
	if found != cronrcPath {
		t.Fatalf("expected %q, got %q", cronrcPath, found)
	}

	// ParseFile test
	file, err := ParseFile(cronrcPath)
	if err != nil {
		t.Fatalf("unexpected ParseFile error: %v", err)
	}
	if len(file.Tasks) != 1 || file.Tasks[0].Command != "echo hi" {
		t.Fatalf("unexpected parsed file: %+v", file)
	}
	if file.Hash == "" {
		t.Fatal("expected non-empty hash")
	}
}
