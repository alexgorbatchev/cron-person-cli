package main

import (
	"os"
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
				if err := os.Mkdir(state+".tmp", 0o700); err != nil {
					t.Fatal(err)
				}
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
