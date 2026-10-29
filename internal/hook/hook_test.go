package hook

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func TestHookScript_Shells(t *testing.T) {
	shells := []string{"bash", "zsh", "fish"}
	for _, shell := range shells {
		script, err := GenerateHookScript(shell, "cron-person")
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", shell, err)
		}
		if !strings.Contains(script, "cron-person") {
			t.Errorf("script for %s missing binary name: %s", shell, script)
		}
	}
}

func TestHookExport_UnauthorizedAndChanged(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "allowed.json")
	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	appDir := filepath.Join(tempDir, "app")
	_ = os.MkdirAll(appDir, 0o755)
	cronrcPath := filepath.Join(appDir, ".cronrc")
	_ = os.WriteFile(cronrcPath, []byte("@hourly ./run.sh\n"), 0o644)

	// 1. Unauthorized
	var stderr bytes.Buffer
	err = HandleHookExport(st, appDir, false, &stderr)
	if err != nil {
		t.Fatalf("unexpected HandleHookExport error: %v", err)
	}
	if !strings.Contains(stderr.String(), "unauthorized") {
		t.Errorf("expected unauthorized notice in stderr, got: %s", stderr.String())
	}

	// 2. Allow
	_, _ = st.Allow(appDir)
	stderr.Reset()
	err = HandleHookExport(st, appDir, false, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected clean output for allowed dir, got: %s", stderr.String())
	}

	// 3. Modified
	_ = os.WriteFile(cronrcPath, []byte("@daily ./run.sh\n"), 0o644)
	stderr.Reset()
	err = HandleHookExport(st, appDir, false, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "modified") && !strings.Contains(stderr.String(), "changed") {
		t.Errorf("expected modified/changed notice in stderr, got: %s", stderr.String())
	}
}

func TestHelperHookExport(t *testing.T) {
	if os.Getenv("GO_WANT_HOOK_EXPORT_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) < 2 || args[0] != "hook" || args[1] != "export" {
		os.Exit(2)
	}
	st, err := store.NewStore(os.Getenv("TEST_HOOK_STORE"))
	if err != nil {
		os.Exit(3)
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(4)
	}
	if err := HandleHookExport(st, cwd, os.Getenv("AGENT") == "1", os.Stderr); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func TestGeneratedHookScriptsDeliverWarnings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, err := GenerateHookScript(shell, "cron-person")
		if err != nil {
			t.Fatalf("GenerateHookScript(%s) error: %v", shell, err)
		}
		if strings.Contains(script, "2>/dev/null") {
			t.Errorf("%s hook script still discards stderr: %s", shell, script)
		}
		if strings.Contains(script, `eval "$`) || strings.Contains(script, "| source") {
			t.Errorf("%s hook script unnecessarily evaluates stdout as shell code: %s", shell, script)
		}
	}

	testExe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	baseDir := t.TempDir()
	wrapperPath := filepath.Join(baseDir, "cron-person")
	wrapperScript := "#!/bin/sh\nexec '" + strings.ReplaceAll(testExe, "'", `'"'"'`) + "' -test.run=^TestHelperHookExport$ -- \"$@\"\n"
	if err := os.WriteFile(wrapperPath, []byte(wrapperScript), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		shell string
		args  []string
		call  string
	}{
		{"bash", []string{"--noprofile", "--norc", "-c"}, "_cron_person_hook"},
		{"zsh", []string{"-f", "-c"}, "_cron_person_hook"},
		{"fish", []string{"--no-config", "-c"}, "__cron_person_export_eval"},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			shellBin, err := exec.LookPath(tc.shell)
			if err != nil {
				t.Skipf("%s not installed", tc.shell)
			}

			statePath := filepath.Join(t.TempDir(), "allowed.json")
			st, err := store.NewStore(statePath)
			if err != nil {
				t.Fatal(err)
			}

			projectDir := filepath.Join(t.TempDir(), "project")
			nestedDir := filepath.Join(projectDir, "sub", "nested")
			if err := os.MkdirAll(nestedDir, 0o755); err != nil {
				t.Fatal(err)
			}
			cronrcPath := filepath.Join(projectDir, ".cronrc")
			if err := os.WriteFile(cronrcPath, []byte("@hourly echo ok\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			hookScript, err := GenerateHookScript(tc.shell, wrapperPath)
			if err != nil {
				t.Fatal(err)
			}

			invokeHook := func() (string, string) {
				t.Helper()
				cmdArgs := append(append([]string{}, tc.args...), hookScript+"\n"+tc.call)
				cmd := exec.Command(shellBin, cmdArgs...)
				cmd.Dir = nestedDir
				cmd.Env = append(os.Environ(),
					"GO_WANT_HOOK_EXPORT_HELPER=1",
					"TEST_HOOK_STORE="+statePath,
					"AGENT=0",
				)
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Fatalf("%s hook failed: %v (stderr=%q)", tc.shell, err, stderr.String())
				}
				return stdout.String(), stderr.String()
			}

			// 1. Unauthorized ancestor .cronrc -> warning on stderr
			stdout, stderr := invokeHook()
			if stdout != "" || !strings.Contains(stderr, "unauthorized .cronrc detected at "+cronrcPath) {
				t.Fatalf("expected unauthorized warning on stderr, got stdout=%q stderr=%q", stdout, stderr)
			}

			// 2. Approved ancestor .cronrc -> quiet
			if _, err := st.Allow(projectDir); err != nil {
				t.Fatal(err)
			}
			stdout, stderr = invokeHook()
			if stdout != "" || stderr != "" {
				t.Fatalf("expected quiet hook for allowed project, got stdout=%q stderr=%q", stdout, stderr)
			}

			// 3. Modified ancestor .cronrc -> warning on stderr
			if err := os.WriteFile(cronrcPath, []byte("@daily echo changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			stdout, stderr = invokeHook()
			if stdout != "" || !strings.Contains(stderr, "modified .cronrc detected at "+cronrcPath) {
				t.Fatalf("expected modified warning on stderr, got stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func TestGeneratedHookScriptsPreserveExitStatusTrapsAndIdempotency(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, err := GenerateHookScript(shell, "cron-person")
		if err != nil {
			t.Fatalf("GenerateHookScript(%s) error: %v", shell, err)
		}
		if strings.Contains(script, "trap ") {
			t.Errorf("%s hook script modifies signal traps: %s", shell, script)
		}
		if !strings.Contains(script, "previous_exit_status") {
			t.Errorf("%s hook script does not preserve previous_exit_status: %s", shell, script)
		}
	}

	testExe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	baseDir := t.TempDir()
	wrapperPath := filepath.Join(baseDir, "cron-person")
	wrapperScript := "#!/bin/sh\nexec '" + strings.ReplaceAll(testExe, "'", `'"'"'`) + "' -test.run=^TestHelperHookExport$ -- \"$@\"\n"
	if err := os.WriteFile(wrapperPath, []byte(wrapperScript), 0o755); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(baseDir, "allowed.json")
	projectDir := filepath.Join(baseDir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		shell         string
		args          []string
		checkSnippet  string
		wantCountLine string
	}{
		{
			shell: "bash",
			args:  []string{"--noprofile", "--norc", "-c"},
			checkSnippet: `trap 'echo prior-int-handler' INT
__HOOK_SCRIPT__
__HOOK_SCRIPT__
(exit 17)
_cron_person_hook
printf 'fail_status=%d\n' "$?"
true
_cron_person_hook
printf 'ok_status=%d\n' "$?"
printf 'prompt_cmd=%s\n' "$PROMPT_COMMAND"
trap -p INT
`,
			wantCountLine: "prompt_cmd=_cron_person_hook",
		},
		{
			shell: "zsh",
			args:  []string{"-f", "-c"},
			checkSnippet: `trap 'echo prior-int-handler' INT
__HOOK_SCRIPT__
__HOOK_SCRIPT__
(exit 17)
_cron_person_hook
printf 'fail_status=%d\n' "$?"
true
_cron_person_hook
printf 'ok_status=%d\n' "$?"
printf 'precmd_count=%d chpwd_count=%d\n' "${#precmd_functions}" "${#chpwd_functions}"
trap
`,
			wantCountLine: "precmd_count=1 chpwd_count=1",
		},
		{
			shell: "fish",
			args:  []string{"--no-config", "-c"},
			checkSnippet: `trap 'echo prior-int-handler' INT
__HOOK_SCRIPT__
__HOOK_SCRIPT__
sh -c 'exit 17'
__cron_person_export_eval
printf 'fail_status=%d\n' $status
true
__cron_person_export_eval
printf 'ok_status=%d\n' $status
printf 'fish_fn=%d\n' (functions -q __cron_person_export_eval; echo $status)
trap
`,
			wantCountLine: "fish_fn=0",
		},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			shellBin, err := exec.LookPath(tc.shell)
			if err != nil {
				t.Skipf("%s not installed", tc.shell)
			}

			hookScript, err := GenerateHookScript(tc.shell, wrapperPath)
			if err != nil {
				t.Fatal(err)
			}

			script := strings.ReplaceAll(tc.checkSnippet, "__HOOK_SCRIPT__", hookScript)
			cmd := exec.Command(shellBin, append(append([]string{}, tc.args...), script)...)
			cmd.Dir = projectDir
			cmd.Env = append(os.Environ(),
				"GO_WANT_HOOK_EXPORT_HELPER=1",
				"TEST_HOOK_STORE="+statePath,
				"AGENT=0",
			)
			outBytes, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s lifecycle check failed: %v\noutput:\n%s", tc.shell, err, outBytes)
			}
			out := string(outBytes)
			for _, want := range []string{
				"fail_status=17",
				"ok_status=0",
				tc.wantCountLine,
				"prior-int-handler",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s output missing %q:\n%s", tc.shell, want, out)
				}
			}
		})
	}
}
