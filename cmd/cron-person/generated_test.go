package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run the real CLI through cron's outer shell, rather than inspecting output text.
func TestGeneratedCronExecution(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "cron-person")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	for _, tt := range []struct{ name, command, want string }{
		{"redirect", `pwd > result`, ""},
		{"environment", `printf "$VERIFY_VALUE" > result`, "from-file"},
		{"operators", `false || printf ok > result; printf done >> result`, "okdone"},
		{"substitution", `printf '%s' "$(pwd)" > result`, ""},
		{"percent", `printf '%s' 'backslash\%value' > result`, `backslash\%value`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			projectName, stateName := "project", "state.json"
			if tt.name == "percent" {
				projectName, stateName = "project's % directory", "custom's % state.json"
			}
			dir := filepath.Join(t.TempDir(), projectName)
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			state := filepath.Join(t.TempDir(), stateName)
			binary := bin
			if tt.name == "percent" {
				binary = filepath.Join(root, "cron person's % binary")
				data, err := os.ReadFile(bin)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(binary, data, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			writeCLIFile(t, filepath.Join(dir, ".cronrc"), "VERIFY_VALUE=from-file\n* * * * * "+tt.command+"\n")
			requireCLIOutput(t, []string{"--store", state, "allow", dir})
			show := exec.Command(binary, "--store", state, "crontab", "show")
			out, err := show.CombinedOutput()
			if err != nil {
				t.Fatalf("show: %v: %s", err, out)
			}
			var command string
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "* * * * * ") {
					command = strings.TrimPrefix(line, "* * * * * ")
				}
			}
			if command == "" {
				t.Fatal("missing cron command")
			}
			// Cron removes the escape from percent signs before passing the
			// command to its shell. An unescaped percent would truncate it.
			for i, c := range command {
				if c == '%' && (i == 0 || command[i-1] != '\\' || (i > 1 && command[i-2] == '\\')) {
					t.Fatalf("unescaped cron percent in %q", command)
				}
			}
			command = strings.ReplaceAll(command, `\%`, "%")
			run := func() ([]byte, error) {
				cmd := exec.Command("/bin/sh", "-c", command)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "SHELL=/bin/sh", "XDG_STATE_HOME="+root+"/empty-state")
				return cmd.CombinedOutput()
			}
			out, err = run()
			if err != nil {
				t.Fatalf("generated command: %v: %s", err, out)
			}
			data, err := os.ReadFile(filepath.Join(dir, "result"))
			want := tt.want
			if tt.name == "redirect" {
				want = dir + "\n"
			}
			if tt.name == "substitution" {
				want = dir
			}
			if err != nil || string(data) != want {
				t.Fatalf("result=%q err=%v want=%q", data, err, want)
			}
			if err := os.Remove(filepath.Join(dir, "result")); err != nil {
				t.Fatal(err)
			}
			for _, status := range []string{"changed", "denied"} {
				if status == "changed" {
					writeCLIFile(t, filepath.Join(dir, ".cronrc"), "# changed\n")
				} else {
					requireCLIOutput(t, []string{"--store", state, "deny", dir})
				}
				out, err = run()
				if err == nil {
					t.Fatalf("%s execution succeeded: %s", status, out)
				}
				for _, path := range []string{filepath.Join(dir, "result"), filepath.Join(root, "result")} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("%s execution created %s: %v", status, path, err)
					}
				}
			}
		})
	}

	t.Run("synced relative store and xdg default under clean cron env", func(t *testing.T) {
		for _, mode := range []string{"relative-flag", "xdg-default"} {
			t.Run(mode, func(t *testing.T) {
				crontabPath := cliCrontab(t)
				writeCLIFile(t, crontabPath, "")
				workDir := t.TempDir()
				cronRunDir := t.TempDir()
				projectDir := filepath.Join(workDir, "project")
				if err := os.MkdirAll(projectDir, 0o700); err != nil {
					t.Fatal(err)
				}
				writeCLIFile(t, filepath.Join(projectDir, ".cronrc"), "VERIFY_VALUE=from-cronrc\n@hourly printf '%s' \"$VERIFY_VALUE\" > result\n")

				t.Chdir(workDir)
				syncXDG := filepath.Join(workDir, "sync-xdg")
				t.Setenv("XDG_STATE_HOME", syncXDG)

				var storeFlags []string
				if mode == "relative-flag" {
					storeFlags = []string{"--store", filepath.Join("rel-store", "allowed.json")}
				}

				requireCLIOutput(t, append(append([]string{}, storeFlags...), "allow", projectDir))
				syncCmd := exec.Command(bin, append(append([]string{}, storeFlags...), "crontab", "sync")...)
				syncCmd.Dir = workDir
				syncCmd.Env = os.Environ()
				if out, err := syncCmd.CombinedOutput(); err != nil {
					t.Fatalf("crontab sync failed: %v: %s", err, out)
				}

				installed, err := os.ReadFile(crontabPath)
				if err != nil {
					t.Fatal(err)
				}
				var cronCmd string
				for _, line := range strings.Split(string(installed), "\n") {
					if strings.HasPrefix(line, "@hourly ") {
						cronCmd = strings.TrimPrefix(line, "@hourly ")
						break
					}
				}
				if cronCmd == "" {
					t.Fatalf("missing @hourly line in installed crontab:\n%s", installed)
				}

				// Execute from a different directory with XDG_STATE_HOME pointing to an empty directory.
				cronCmd = strings.ReplaceAll(cronCmd, `\%`, "%")
				cmd := exec.Command("/bin/sh", "-c", cronCmd)
				cmd.Dir = cronRunDir
				cmd.Env = append(os.Environ(), "SHELL=/bin/sh", "XDG_STATE_HOME="+filepath.Join(cronRunDir, "clean-xdg"))
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("scheduled command failed under clean cron env: %v: %s", err, out)
				}
				got, err := os.ReadFile(filepath.Join(projectDir, "result"))
				if err != nil || string(got) != "from-cronrc" {
					t.Fatalf("result=%q err=%v, want %q", got, err, "from-cronrc")
				}
			})
		}
	})
}
