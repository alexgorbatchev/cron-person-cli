package cronrc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_StandardCrontab(t *testing.T) {
	input := `
# Project level cron tasks
SHELL=/bin/bash
PROJECT_ENV=production
SPACED_VAR = spaced value
QUOTED_SPACED = "  keep spaces  "

# Run backup every hour
0 * * * * ./scripts/backup.sh --full

# Clean temporary files every day at midnight
@daily make clean-tmp

# Every 15 minutes sync
*/15 * * * * sync-worker --queue=default

# Weekly on Sunday using standard crontab day-of-week 7
0 0 * * 7 ./worker.sh

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
	if file.Env["SPACED_VAR"] != "spaced value" {
		t.Errorf("expected SPACED_VAR='spaced value', got %q", file.Env["SPACED_VAR"])
	}
	if file.Env["QUOTED_SPACED"] != "  keep spaces  " {
		t.Errorf("expected QUOTED_SPACED='  keep spaces  ', got %q", file.Env["QUOTED_SPACED"])
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
	if file.Tasks[3].Schedule != "0 0 * * 7" || file.Tasks[3].Command != "./worker.sh" {
		t.Errorf("task 3 mismatch: %+v", file.Tasks[3])
	}
	if file.Tasks[4].Schedule != "@reboot" || file.Tasks[4].Command != "./startup.sh" {
		t.Errorf("task 4 mismatch: %+v", file.Tasks[4])
	}
}

func TestParse_PreservesQuotedWhitespaceAndEscapedSeparators(t *testing.T) {
	input := "@hourly   echo \"a  b\tc\" 'x   y' foo\\ \\ bar ; echo \"1  2\"\n" +
		"0\t*\t*\t*\t*\tprintf '%s\\n' \"two  spaces\" 'tab\there' arg\\;escaped\n"

	file, err := Parse(strings.NewReader(input), "/tmp/project/.cronrc")
	if err != nil {
		t.Fatalf("unexpected Parse error: %v", err)
	}
	if len(file.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(file.Tasks))
	}

	want0 := `echo "a  b	c" 'x   y' foo\ \ bar ; echo "1  2"`
	if file.Tasks[0].Schedule != "@hourly" || file.Tasks[0].Command != want0 {
		t.Fatalf("task 0 schedule=%q command=%q, want command=%q", file.Tasks[0].Schedule, file.Tasks[0].Command, want0)
	}

	want1 := `printf '%s\n' "two  spaces" 'tab	here' arg\;escaped`
	if file.Tasks[1].Schedule != "0 * * * *" || file.Tasks[1].Command != want1 {
		t.Fatalf("task 1 schedule=%q command=%q, want command=%q", file.Tasks[1].Schedule, file.Tasks[1].Command, want1)
	}
}

func TestParse_AcceptedScheduleFamiliesAndNativeSyntaxCheck(t *testing.T) {
	schedules := []string{
		"0 * * * *",
		"*/15 * * * *",
		"0 0 * * 0",
		"0 0 * * 7",
		"0 0 * * 1-7",
		"0 0 * * 0-7/2",
		"0 0 * * 5,7",
		"0 0 * * 7-7",
		"0 0 * * 7-7/1",
		"@hourly",
		"@daily",
		"@midnight",
		"@weekly",
		"@monthly",
		"@yearly",
		"@annually",
		"@reboot",
	}

	var cronrcBuilder strings.Builder
	cronrcBuilder.WriteString("VALUE = hello\n")
	for _, sched := range schedules {
		cronrcBuilder.WriteString(sched + " true\n")
	}

	parsed, err := Parse(strings.NewReader(cronrcBuilder.String()), "/tmp/project/.cronrc")
	if err != nil {
		t.Fatalf("expected all standard schedule families to parse, got error: %v", err)
	}
	if len(parsed.Tasks) != len(schedules) {
		t.Fatalf("tasks=%d, want %d", len(parsed.Tasks), len(schedules))
	}

	if crontabBin, err := exec.LookPath("crontab"); err == nil {
		helpOut, _ := exec.Command(crontabBin, "-h").CombinedOutput()
		if strings.Contains(string(helpOut), "-n") {
			var crontabLines strings.Builder
			for _, task := range parsed.Tasks {
				crontabLines.WriteString(task.Schedule + " " + task.Command + "\n")
			}
			tmpDir := t.TempDir()
			tmpCrontab := filepath.Join(tmpDir, "test.crontab")
			if err := os.WriteFile(tmpCrontab, []byte(crontabLines.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			checkCmd := exec.Command(crontabBin, "-n", "test.crontab")
			checkCmd.Dir = tmpDir
			if out, err := checkCmd.CombinedOutput(); err != nil {
				t.Fatalf("native crontab -n rejected accepted schedules: %v: %s", err, out)
			}
		}
	}
}

func TestParse_InvalidSchedule(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"invalid fields", "invalid schedule string here", "expected at least 5 schedule fields"},
		{"missing command for descriptor", "@daily", "expected schedule and command"},
		{"unsupported @every interval", "@every 2h ./worker.sh", "unsupported @every"},
		{"missing duration for @every", "@every", "expected schedule and command"},
		{"unknown descriptor", "@fortnightly ./worker.sh", "invalid schedule descriptor"},
		{"invalid cron format", "99 99 99 99 99 echo bad", "invalid cron schedule"},
		{"invalid dow 8", "0 0 * * 8 echo bad", "invalid cron schedule"},
		{"invalid dow range 7-5", "0 0 * * 7-5 echo bad", "invalid cron schedule"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input), "/tmp/.cronrc")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("for %q: error=%v, want containing %q", tt.input, err, tt.wantErr)
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

	// Create .cronrc in a/
	cronrcPath := filepath.Join(tempDir, "a", ".cronrc")
	if err := os.WriteFile(cronrcPath, []byte("@daily echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A directory named .cronrc must not hide the nearest configuration file.
	if err := os.Mkdir(filepath.Join(subDir, ".cronrc"), 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := FindCronrc(subDir)
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
