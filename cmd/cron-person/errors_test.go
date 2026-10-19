package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLICorruptStoreErrors(t *testing.T) {
	for _, args := range [][]string{
		{"allow"}, {"deny"}, {"dir", "list"}, {"dir", "status"},
		{"task", "list"}, {"task", "run", ".", "true"},
		{"exec", ".", "true"}, {"crontab", "sync"}, {"crontab", "show"},
		{"hook", "export"}, {"status"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			writeCLIFile(t, path, "invalid json")
			out, err := executeCommand(append([]string{"--store", path}, args...)...)
			if err == nil || !strings.Contains(err.Error(), "loading store") {
				t.Fatalf("command=%v error=%v output=%q", args, err, out)
			}
		})
	}
}

func TestCLIDefaultStoreResolutionFailure(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	out, err := executeCommand("status")
	if err == nil || !strings.Contains(err.Error(), "resolving store path") {
		t.Fatalf("error=%v output=%q", err, out)
	}
}

func TestCLIUnavailableWorkingDirectory(t *testing.T) {
	for _, args := range [][]string{
		{"allow"}, {"deny"}, {"dir", "check"}, {"dir", "status"},
		{"allow", "relative"}, {"task", "run", "relative", "true"},
		{"exec", "relative", "true"}, {"hook", "export"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			cwd := filepath.Join(dir, "removed")
			if err := os.Mkdir(cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(cwd)
			if err := os.Remove(cwd); err != nil {
				t.Fatal(err)
			}
			out, err := executeCommand(append([]string{"--store", path}, args...)...)
			if err == nil || !strings.Contains(err.Error(), "getwd") {
				t.Fatalf("command=%v error=%v output=%q", args, err, out)
			}
		})
	}
}

func TestCLIDirectoryFailures(t *testing.T) {
	for _, tt := range []struct{ name, verb, want string }{
		{"invalid schedule", "allow", "allowing directory"},
		{"invalid syntax", "check", "invalid .cronrc syntax"},
		{"allow persistence", "allow", "writing store temp file"},
		{"deny persistence", "deny", "denying directory"},
		{"status filesystem", "status", "checking .cronrc"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir, state := cliProject(t)
			path := filepath.Join(dir, ".cronrc")
			switch tt.name {
			case "invalid schedule", "invalid syntax":
				writeCLIFile(t, path, "invalid schedule\n")
			case "allow persistence", "deny persistence":
				writeCLIFile(t, state+".lock", "")
				stateDir := filepath.Dir(state)
				if err := os.Chmod(stateDir, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })
			case "status filesystem":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(".cronrc", path); err != nil {
					t.Fatal(err)
				}
			}
			out, err := executeCommand("--store", state, "dir", tt.verb, dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v output=%q, want %q", err, out, tt.want)
			}
		})
	}
}

func TestCLIArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{"allow", "one", "two"}, {"deny", "one", "two"},
		{"dir", "list", "extra"}, {"dir", "status", "one", "two"},
		{"dir", "check", "one", "two"}, {"task", "run"},
		{"exec", "."}, {"exec", ".", "--", "--"},
		{"hook", "unsupported"}, {"hook"}, {"hook", "export", "one", "two"},
		{"status", "extra"}, {"crontab", "sync", "extra"},
		{"crontab", "uninstall", "extra"}, {"--unknown"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, state := cliProject(t)
			out, err := executeCommand(append([]string{"--store", state}, args...)...)
			if err == nil {
				t.Fatalf("invalid arguments accepted: %v, output=%q", args, out)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("stdout write failed")
}

func TestCLIOutputWriteFailures(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run("agent="+mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			crontabPath := cliCrontab(t)
			writeCLIFile(t, crontabPath, "")
			dir, state := cliProject(t)

			runWithFailingOut := func(args ...string) error {
				cmd := newRootCommand()
				cmd.SetOut(failingWriter{})
				cmd.SetErr(new(bytes.Buffer))
				cmd.SetArgs(args)
				return cmd.Execute()
			}

			// Commands that write to stdout before any directory is allowed.
			for _, args := range [][]string{
				{"--store", state, "status"},
				{"--store", state, "dir", "list"},
				{"--store", state, "dir", "check", dir},
				{"--store", state, "dir", "status", dir},
				{"--store", state, "task", "list"},
				{"--store", state, "crontab", "show"},
				{"--store", state, "crontab", "sync"},
				{"--store", state, "crontab", "uninstall"},
				{"--store", state, "allow", dir},
				{"hook", "bash"},
			} {
				if err := runWithFailingOut(args...); err == nil || !strings.Contains(err.Error(), "stdout write failed") {
					t.Fatalf("expected stdout write error for %v, got %v", args, err)
				}
			}

			// Now allow dir with working stdout so we can test populated list/status/task list and deny.
			requireCLIOutput(t, []string{"--store", state, "allow", dir})
			for _, args := range [][]string{
				{"--store", state, "status"},
				{"--store", state, "dir", "list"},
				{"--store", state, "dir", "status", dir},
				{"--store", state, "task", "list"},
				{"--store", state, "deny", dir},
			} {
				if err := runWithFailingOut(args...); err == nil || !strings.Contains(err.Error(), "stdout write failed") {
					t.Fatalf("expected stdout write error for populated %v, got %v", args, err)
				}
			}

			// Also verify hook export propagates stderr write failures for unauthorized and modified .cronrc.
			t.Chdir(dir)
			cmd := newRootCommand()
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(failingWriter{})
			cmd.SetArgs([]string{"--store", state, "hook", "export", "bash"})
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "stdout write failed") {
				t.Fatalf("expected stderr write error for unauthorized hook export, got %v", err)
			}
			requireCLIOutput(t, []string{"--store", state, "allow", dir})
			writeCLIFile(t, filepath.Join(dir, ".cronrc"), "@daily echo changed\n")
			cmd = newRootCommand()
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(failingWriter{})
			cmd.SetArgs([]string{"--store", state, "hook", "export", "bash"})
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "stdout write failed") {
				t.Fatalf("expected stderr write error for modified hook export, got %v", err)
			}
		})
	}

	if _, err := os.Stat("/dev/full"); err == nil {
		bin := filepath.Join(t.TempDir(), "cron-person")
		build := exec.Command("go", "build", "-o", bin, ".")
		build.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build: %v: %s", err, out)
		}
		state := filepath.Join(t.TempDir(), "state.json")
		for _, mode := range []string{"0", "1"} {
			shCmd := exec.Command("/bin/sh", "-c", "'"+strings.ReplaceAll(bin, "'", `'"'"'`)+"' --store '"+strings.ReplaceAll(state, "'", `'"'"'`)+"' status > /dev/full")
			shCmd.Env = append(os.Environ(), "AGENT="+mode)
			if err := shCmd.Run(); err == nil {
				t.Fatalf("expected non-zero exit when writing status to /dev/full in AGENT=%s", mode)
			}
		}
	}
}
