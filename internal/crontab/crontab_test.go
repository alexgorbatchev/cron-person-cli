package crontab

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/runner"
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

func TestHelperCronPersonRunner(t *testing.T) {
	if os.Getenv("GO_WANT_CRON_PERSON_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) != 6 || args[0] != "--store" || args[2] != "exec" || args[4] != "--" {
		os.Exit(2)
	}
	st, err := store.NewStore(args[1])
	if err != nil {
		os.Exit(3)
	}
	err = runner.NewRunner(st).Exec(context.Background(), runner.ExecOptions{
		Dir:     args[3],
		Command: args[5],
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	})
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

// cronPreprocessCommand simulates vixie-cron/Debian cron(8) command-field percent and backslash processing.
func cronPreprocessCommand(cmdField string) (string, string) {
	var cmdBuf, stdinBuf strings.Builder
	escaped := false
	inStdin := false
	for i := 0; i < len(cmdField); i++ {
		ch := cmdField[i]
		if escaped {
			escaped = false
			if ch != '%' && ch != '\\' {
				if inStdin {
					stdinBuf.WriteByte('\\')
				} else {
					cmdBuf.WriteByte('\\')
				}
			}
			if inStdin {
				stdinBuf.WriteByte(ch)
			} else {
				cmdBuf.WriteByte(ch)
			}
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '%' {
			if inStdin {
				stdinBuf.WriteByte('\n')
			} else {
				inStdin = true
			}
			continue
		}
		if inStdin {
			stdinBuf.WriteByte(ch)
		} else {
			cmdBuf.WriteByte(ch)
		}
	}
	if escaped {
		if inStdin {
			stdinBuf.WriteByte('\\')
		} else {
			cmdBuf.WriteByte('\\')
		}
	}
	return cmdBuf.String(), stdinBuf.String()
}

func TestGenerateBlock_ExecutesSafelyAndBlocksUnauthorized(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	baseDir := t.TempDir()
	outerDir := filepath.Join(baseDir, "outer-cwd")
	if err := os.MkdirAll(outerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(baseDir, "allowed.json")
	st, err := store.NewStore(statePath)
	if err != nil {
		t.Fatal(err)
	}

	testExe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(baseDir, "bin with 'spaces' $OUTER_VAR 100%")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wrapperPath := filepath.Join(binDir, "cron-person")
	wrapperScript := "#!/bin/sh\nexec '" + strings.ReplaceAll(testExe, "'", `'"'"'`) + "' -test.run=^TestHelperCronPersonRunner$ -- \"$@\"\n"
	if err := os.WriteFile(wrapperPath, []byte(wrapperScript), 0o755); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(baseDir, "project with 'spaces' $OUTER_VAR; 100%")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(baseDir, "side-effect-marker")
	cronrcPath := filepath.Join(projectDir, ".cronrc")
	cronrcContent := "APP_VALUE=from-cronrc\n" +
		`@hourly printf '%s\n' "$PWD" "$APP_VALUE" 'single-quoted' "$(printf sub-ok)" '100%' '\%' '\\' | cat; printf 'effect' > "` + markerPath + `"` + "\n"
	if err := os.WriteFile(cronrcPath, []byte(cronrcContent), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := st.Allow(projectDir)
	if err != nil {
		t.Fatal(err)
	}

	block, err := GenerateBlock([]store.Record{*rec}, wrapperPath, statePath)
	if err != nil {
		t.Fatal(err)
	}

	var cronCmd string
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "@hourly ") {
			cronCmd = strings.TrimPrefix(line, "@hourly ")
			break
		}
	}
	if cronCmd == "" {
		t.Fatalf("missing @hourly entry in generated block:\n%s", block)
	}

	runEntry := func(shellCmd string, stdin string) (string, string, error) {
		cmd := exec.Command("/bin/sh", "-c", shellCmd)
		cmd.Dir = outerDir
		cmd.Stdin = strings.NewReader(stdin)
		cmd.Env = append(os.Environ(),
			"GO_WANT_CRON_PERSON_HELPER=1",
			"TEST_CRON_PERSON_STORE="+statePath,
			"APP_VALUE=wrong-outer-env",
			"OUTER_VAR=expanded-wrong",
			"SHELL=/bin/sh",
		)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		return stdout.String(), stderr.String(), runErr
	}

	for _, mode := range []string{"direct-sh", "cron-preprocessed"} {
		t.Run(mode, func(t *testing.T) {
			_ = os.Remove(markerPath)
			shellCmd := cronCmd
			stdinData := ""
			if mode == "cron-preprocessed" {
				shellCmd, stdinData = cronPreprocessCommand(cronCmd)
				if stdinData != "" {
					t.Fatalf("expected cron preprocessing not to split command into stdin, got stdin=%q", stdinData)
				}
			}

			stdout, stderr, err := runEntry(shellCmd, stdinData)
			if err != nil {
				t.Fatalf("expected authorized run to succeed, err=%v stderr=%q", err, stderr)
			}
			wantStdout := strings.Join([]string{
				projectDir,
				"from-cronrc",
				"single-quoted",
				"sub-ok",
				"100%",
				`\%`,
				`\\`,
				"",
			}, "\n")
			if stdout != wantStdout {
				t.Fatalf("stdout=%q, want %q", stdout, wantStdout)
			}
			markerData, err := os.ReadFile(markerPath)
			if err != nil || string(markerData) != "effect" {
				t.Fatalf("expected side effect while authorized, data=%q err=%v", markerData, err)
			}
		})
	}

	t.Run("revoked", func(t *testing.T) {
		_ = os.Remove(markerPath)
		if err := st.Deny(projectDir); err != nil {
			t.Fatal(err)
		}
		if stdout, stderr, err := runEntry(cronCmd, ""); err == nil {
			t.Fatalf("expected revoked entry to fail, stdout=%q stderr=%q", stdout, stderr)
		}
		if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
			t.Fatalf("revoked entry executed side effect: stat err=%v", err)
		}
	})

	t.Run("tampered", func(t *testing.T) {
		_ = os.Remove(markerPath)
		if _, err := st.Allow(projectDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cronrcPath, []byte(cronrcContent+"# tampered\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if stdout, stderr, err := runEntry(cronCmd, ""); err == nil {
			t.Fatalf("expected tampered entry to fail, stdout=%q stderr=%q", stdout, stderr)
		}
		if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
			t.Fatalf("tampered entry executed side effect: stat err=%v", err)
		}
	})
}

func TestGenerateBlock_RejectsInvalidControlCharacters(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state.json")
	validCronrc := filepath.Join(dir, ".cronrc")
	if err := os.WriteFile(validCronrc, []byte("@hourly echo ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	validRecord := store.Record{Dir: dir, Path: validCronrc}

	if _, err := GenerateBlock([]store.Record{validRecord}, "/bin/cron-person\ninjected", state); err == nil {
		t.Fatal("expected error for newline in binPath")
	}
	if _, err := GenerateBlock([]store.Record{validRecord}, "/bin/cron-person", ""); err == nil {
		t.Fatal("expected error for empty storePath")
	}
	if _, err := GenerateBlock([]store.Record{{Dir: "", Path: validCronrc}}, "/bin/cron-person", state); err == nil {
		t.Fatal("expected error for empty record Dir")
	}
	if _, err := GenerateBlock([]store.Record{{Dir: dir + "\ninjected", Path: validCronrc}}, "/bin/cron-person", state); err == nil {
		t.Fatal("expected error for newline in record Dir")
	}

	if _, err := GenerateBlock([]store.Record{{Dir: dir + "\rinjected", Path: validCronrc}}, "/bin/cron-person", state); err == nil {
		t.Fatal("expected error for carriage return in record Dir")
	}

	nulCronrc := filepath.Join(dir, "nul.cronrc")
	if err := os.WriteFile(nulCronrc, []byte("@hourly echo ok\x00malicious\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateBlock([]store.Record{{Dir: dir, Path: nulCronrc}}, "/bin/cron-person", state); err == nil {
		t.Fatal("expected error for NUL byte in command")
	}
}
