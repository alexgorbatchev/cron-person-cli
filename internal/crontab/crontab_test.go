package crontab

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func TestMergeCrontab_NewBlock(t *testing.T) {
	existing := `MAILTO=admin@example.com
0 0 * * * /usr/bin/custom-script.sh
`
	managedBlock := `# >>> cron-person managed block (DO NOT EDIT DIRECTLY) >>>
@hourly /path/to/cron-person exec "/var/app" -- ./run.sh
# <<< cron-person managed block <<<`

	merged := MergeCrontab(existing, managedBlock)

	if !strings.HasPrefix(merged, "MAILTO=admin@example.com") {
		t.Errorf("expected existing crontab retained, got: %s", merged)
	}
	if !strings.Contains(merged, managedBlock) {
		t.Errorf("expected managed block inserted, got: %s", merged)
	}
}

func TestQuoteCronArgRoundTrip(t *testing.T) {
	for _, input := range []string{"", "plain", "spaces and 'quotes'", `dollar$backtick` + "`" + `slash\`, `%`, `\%`, `\\%`, "line\nbreak"} {
		t.Run(input, func(t *testing.T) {
			arg := strings.ReplaceAll(quoteCronArg(input), `\%`, "%")
			cmd := exec.Command("/bin/sh", "-c", "printf '%s' "+arg)
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != input {
				t.Fatalf("round trip=%q err=%v want=%q", out, err, input)
			}
		})
	}
}

func TestGenerateBlockRejectsInvalidPaths(t *testing.T) {
	for _, tt := range []struct{ name, bin, state, dir string }{
		{"executable", "bad\nbin", "state", "dir"},
		{"state", "bin", "bad\rstate", "dir"},
		{"directory", "bin", "state", "bad\ndir"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			block, err := GenerateBlock([]store.Record{{Dir: tt.dir}}, tt.bin, tt.state)
			if err == nil || block != "" {
				t.Fatalf("unsafe block=%q err=%v", block, err)
			}
		})
	}
}

func TestMergeCrontab_ReplaceExistingBlock(t *testing.T) {
	existing := `0 0 * * * /usr/bin/custom-script.sh

# >>> cron-person managed block (DO NOT EDIT DIRECTLY) >>>
@daily /old/cron-person exec "/old/dir" -- ./old.sh
# <<< cron-person managed block <<<

# Another user cron job
30 4 * * * /usr/bin/cleanup.sh
`

	newBlock := `# >>> cron-person managed block (DO NOT EDIT DIRECTLY) >>>
@hourly /new/cron-person exec "/new/dir" -- ./new.sh
# <<< cron-person managed block <<<`

	merged := MergeCrontab(existing, newBlock)

	if strings.Contains(merged, "@daily /old/cron-person") {
		t.Errorf("expected old block to be removed, got: %s", merged)
	}
	if !strings.Contains(merged, "@hourly /new/cron-person") {
		t.Errorf("expected new block to be inserted, got: %s", merged)
	}
	if !strings.Contains(merged, "30 4 * * * /usr/bin/cleanup.sh") {
		t.Errorf("expected bottom user cron job preserved, got: %s", merged)
	}
}

func TestRemoveBlock(t *testing.T) {
	existing := `0 0 * * * /usr/bin/custom-script.sh

# >>> cron-person managed block (DO NOT EDIT DIRECTLY) >>>
@daily /old/cron-person exec "/old/dir" -- ./old.sh
# <<< cron-person managed block <<<

30 4 * * * /usr/bin/cleanup.sh
`
	removed := RemoveBlock(existing)
	if strings.Contains(removed, "cron-person managed block") {
		t.Errorf("expected managed block removed, got: %s", removed)
	}
	if !strings.Contains(removed, "0 0 * * * /usr/bin/custom-script.sh") || !strings.Contains(removed, "30 4 * * * /usr/bin/cleanup.sh") {
		t.Errorf("expected original jobs preserved, got: %s", removed)
	}
}

func TestGenerateBlock(t *testing.T) {
	tempDir := t.TempDir()
	appDir := filepath.Join(tempDir, "app")
	_ = os.MkdirAll(appDir, 0o755)
	_ = os.WriteFile(filepath.Join(appDir, ".cronrc"), []byte("0 * * * * ./task.sh\n@daily make sync\n"), 0o644)

	records := []store.Record{
		{
			Dir:  appDir,
			Path: filepath.Join(appDir, ".cronrc"),
		},
	}

	state := filepath.Join(tempDir, "state.json")
	block, err := GenerateBlock(records, "/usr/local/bin/cron-person", state)
	if err != nil {
		t.Fatalf("unexpected GenerateBlock error: %v", err)
	}

	if !strings.Contains(block, "0 * * * * '/usr/local/bin/cron-person' --store '"+state+"' exec '"+appDir+"' -- './task.sh'") {
		t.Errorf("expected task 1 in block, got: %s", block)
	}
	if !strings.Contains(block, "@daily '/usr/local/bin/cron-person' --store '"+state+"' exec '"+appDir+"' -- 'make sync'") {
		t.Errorf("expected task 2 in block, got: %s", block)
	}
}
