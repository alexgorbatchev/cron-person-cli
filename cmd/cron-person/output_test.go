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
				{"allow", dir}, {"dir", "check", dir}, {"dir", "list"},
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
			for _, command := range [][]string{{"task", "list"}, {"dir", "list"}, {"dir", "status"}, {"status"}, {"crontab", "show"}, {"crontab", "sync"}} {
				if failure == "parsing" && strings.Join(command, " ") == "dir list" {
					continue // Directory listings inspect authorization without parsing tasks.
				}
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
