package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func TestCLIHomePaths(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run("agent="+mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			dir, state := cliProject(t)
			t.Setenv("HOME", dir)
			for _, args := range [][]string{
				{"allow", dir}, {"dir", "check", dir}, {"status"},
				{"dir", "status", dir}, {"task", "list"}, {"deny", dir},
			} {
				out := requireCLIOutput(t, append([]string{"--store", state}, args...), "~")
				if strings.Contains(out, dir) {
					t.Errorf("%v leaked absolute home: %s", args, out)
				}
			}
			t.Chdir(dir)
			requireCLIOutput(t, []string{"--store", state, "hook", "export"}, "~/.cronrc")
			requireCLIOutput(t, []string{"--store", state, "allow", dir})
			writeCLIFile(t, filepath.Join(dir, ".cronrc"), "@hourly true\n")
			requireCLIOutput(t, []string{"--store", state, "hook", "export"}, "~/.cronrc")

			// Executable output must retain absolute paths and exact command bytes.
			requireCLIOutput(t, []string{"--store", state, "allow", dir})
			requireCLIOutput(t, []string{"--store", state, "crontab", "show"}, dir, state)
			for _, prefix := range [][]string{{"exec"}, {"task", "run"}} {
				args := append([]string{"--store", state}, prefix...)
				args = append(args, dir, `printf '%s' "$PWD"`)
				requireCLIOutput(t, args, dir)
			}
			var stdout, stderr bytes.Buffer
			code := run([]string{"--store", state, "dir", "check", filepath.Join(dir, "missing")}, &stdout, &stderr)
			if code != 1 || stdout.Len() != 0 || strings.Contains(stderr.String(), dir) || !strings.Contains(stderr.String(), "~/missing/.cronrc") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if strings.Count(stderr.String(), modeText(mode, "[ERROR]", "ERR:")) != 1 {
				t.Fatalf("error should be printed once: %s", stderr.String())
			}
			stdout.Reset()
			stderr.Reset()
			if code := run([]string{"--version"}, &stdout, &stderr); code != 0 || stdout.String() != version+"\n" || stderr.Len() != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCommandsReportInspectionAndParseErrors(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		for _, failure := range []string{"inspection", "parsing"} {
			// A command about one directory fails on it; commands over every
			// directory skip it (TestAggregateCommandsSkipBrokenDirectories).
			for _, command := range [][]string{{"dir", "status"}} {
				t.Run(mode+"/"+failure+"/"+strings.Join(command, " "), func(t *testing.T) {
					t.Setenv("AGENT", mode)
					dir, state := cliProject(t)
					installed := cliCrontab(t)
					writeCLIFile(t, installed, "@daily preserve-existing\n")
					t.Setenv("HOME", dir)
					path := filepath.Join(dir, ".cronrc")
					requireCLIOutput(t, []string{"--store", state, "allow", dir})
					want := "checking authorization for ~"
					if failure == "inspection" {
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(".cronrc", path); err != nil {
							t.Fatal(err)
						}
					} else {
						content := "not a valid schedule\n"
						writeCLIFile(t, path, content)
						data, err := json.Marshal([]store.Record{{Dir: dir, Path: path, Hash: cronrc.ComputeHash([]byte(content))}})
						if err != nil {
							t.Fatal(err)
						}
						writeCLIFile(t, state, string(data))
						want = "parsing ~/.cronrc: line 1:"
					}
					var stdout, stderr bytes.Buffer
					args := append([]string{"--store", state}, command...)
					if strings.Join(command, " ") == "dir status" {
						args = append(args, dir)
						if failure == "inspection" {
							want = "checking .cronrc"
						}
					}
					code := run(args, &stdout, &stderr)
					if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), want) || strings.Contains(stderr.String(), dir) {
						t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
					}
					if data, err := os.ReadFile(installed); err != nil || string(data) != "@daily preserve-existing\n" {
						t.Fatalf("failed command changed crontab: %q, %v", data, err)
					}
					if _, err := os.Stat(installed + ".calls"); !os.IsNotExist(err) {
						t.Fatalf("failed command invoked crontab: %v", err)
					}
				})
			}
		}
	}
}

// Commands over every tracked directory name each broken one on stderr and
// still report, preview, or install the healthy directories.
func TestAggregateCommandsSkipBrokenDirectories(t *testing.T) {
	for _, mode := range []struct{ agent, prefix string }{{"0", "[WARN] "}, {"1", "WARN: "}} {
		for _, failure := range []string{"unreadable", "malformed", "carriage return", "changed"} {
			for _, command := range [][]string{{"task", "list"}, {"status"}, {"crontab", "show"}, {"crontab", "sync"}} {
				t.Run(mode.agent+"/"+failure+"/"+strings.Join(command, " "), func(t *testing.T) {
					t.Setenv("AGENT", mode.agent)
					home := t.TempDir()
					t.Setenv("HOME", home)
					t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
					installed := cliCrontab(t)
					writeCLIFile(t, installed, "@daily preserve-existing\n")
					state := filepath.Join(home, "state.json")
					healthy := filepath.Join(home, "healthy")
					broken := filepath.Join(home, "broken")
					for _, dir := range []string{healthy, broken} {
						if err := os.Mkdir(dir, 0o700); err != nil {
							t.Fatal(err)
						}
						writeCLIFile(t, filepath.Join(dir, ".cronrc"), "@daily printf "+filepath.Base(dir)+"\n")
						requireCLIOutput(t, []string{"--store", state, "allow", dir})
					}
					brokenPath := filepath.Join(broken, ".cronrc")
					var want string
					switch failure {
					case "unreadable":
						if err := os.Remove(brokenPath); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(".cronrc", brokenPath); err != nil {
							t.Fatal(err)
						}
						want = "skipped ~/broken: checking .cronrc"
					case "malformed", "carriage return":
						// Approve content the parser rejects, as a grammar change would.
						content := "not a valid schedule\n"
						if failure == "carriage return" {
							content = "@daily printf broken\rmore\n"
						}
						writeCLIFile(t, brokenPath, content)
						records := []store.Record{
							{Dir: broken, Path: brokenPath, Hash: cronrc.ComputeHash([]byte(content))},
							{Dir: healthy, Path: filepath.Join(healthy, ".cronrc"), Hash: cronrc.ComputeHash([]byte("@daily printf healthy\n"))},
						}
						data, err := json.Marshal(records)
						if err != nil {
							t.Fatal(err)
						}
						writeCLIFile(t, state, string(data))
						want = "skipped ~/broken: parsing ~/broken/.cronrc: line 1:"
					case "changed":
						writeCLIFile(t, brokenPath, "@hourly printf edited\n")
						want = "skipped ~/broken: .cronrc changed since it was allowed"
					}

					var stdout, stderr bytes.Buffer
					if code := run(append([]string{"--store", state}, command...), &stdout, &stderr); code != 0 {
						t.Fatalf("exit %d, stderr %q", code, stderr.String())
					}
					if strings.Join(command, " ") == "status" {
						// Status shows the reason on the directory's own row instead.
						reason := strings.TrimPrefix(want, "skipped ~/broken: ")
						if stderr.Len() != 0 || !strings.Contains(stdout.String(), "~/broken") || !strings.Contains(stdout.String(), reason) {
							t.Fatalf("stdout %q stderr %q, want a row for ~/broken with %q", stdout.String(), stderr.String(), reason)
						}
					} else {
						lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
						if len(lines) != 1 || !strings.HasPrefix(lines[0], mode.prefix) || !strings.Contains(lines[0], want) {
							t.Fatalf("stderr %q, want one %q warning containing %q", stderr.String(), mode.prefix, want)
						}
					}
					if out := stdout.String(); strings.Contains(out, "printf broken") || strings.Contains(out, "printf edited") {
						t.Fatalf("broken directory's tasks reported: %q", out)
					}
					switch strings.Join(command, " ") {
					case "task list", "crontab show":
						if !strings.Contains(stdout.String(), "printf healthy") {
							t.Fatalf("healthy task missing: %q", stdout.String())
						}
					case "status":
						if !strings.Contains(stdout.String(), modeText(mode.agent, "Tracked Directories:  2", "tracked_dirs: 2")) ||
							!strings.Contains(stdout.String(), modeText(mode.agent, "Allowed Directories:  1", "allowed_dirs: 1")) {
							t.Fatalf("status counts: %q", stdout.String())
						}
					case "crontab sync":
						data, err := os.ReadFile(installed)
						if err != nil || !strings.HasPrefix(string(data), "@daily preserve-existing\n") || !strings.Contains(string(data), "printf healthy") || strings.Contains(string(data), "broken") {
							t.Fatalf("installed crontab %q, %v; want the healthy directory only", data, err)
						}
						if mode.agent == "1" && !strings.Contains(stdout.String(), "skipped_directories: 1") {
							t.Fatalf("agent output %q lacks the skipped count", stdout.String())
						}
					}
					if strings.Join(command, " ") != "crontab sync" {
						if _, err := os.Stat(installed + ".calls"); !os.IsNotExist(err) {
							t.Fatalf("%v invoked crontab: %v", command, err)
						}
					}
				})
			}
		}
	}
}

// A path with a line break, approved before such paths were refused, must not
// split its warning across lines.
func TestSkippedWarningsStayOnOneLine(t *testing.T) {
	for _, mode := range []struct{ agent, prefix string }{{"0", "[WARN] "}, {"1", "WARN: "}} {
		t.Run("agent="+mode.agent, func(t *testing.T) {
			t.Setenv("AGENT", mode.agent)
			home := t.TempDir()
			t.Setenv("HOME", home)
			project := filepath.Join(home, "line\nbreak")
			if err := os.Mkdir(project, 0o700); err != nil {
				t.Fatal(err)
			}
			content := "@daily true\n"
			writeCLIFile(t, filepath.Join(project, ".cronrc"), content)
			state := filepath.Join(home, "state.json")
			data, err := json.Marshal([]store.Record{{Dir: project, Path: filepath.Join(project, ".cronrc"), Hash: cronrc.ComputeHash([]byte(content))}})
			if err != nil {
				t.Fatal(err)
			}
			writeCLIFile(t, state, string(data))
			var stdout, stderr bytes.Buffer
			if code := run([]string{"--store", state, "task", "list"}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr.String())
			}
			want := mode.prefix + `skipped ~/line\nbreak: directory path contains a line break or NUL, which a crontab entry cannot hold`
			if stderr.String() != want+"\n" {
				t.Fatalf("stderr %q, want %q", stderr.String(), want+"\n")
			}
		})
	}
}
