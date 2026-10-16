package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrap(t *testing.T) {
	for _, name := range []string{"success", "missing shim", "duplicate", "dynamic", "relative", "outside", "missing binaries", "missing payload", "missing source", "empty", "temp blocked", "rename failure"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			payload := filepath.Join(root, ".generated", "binaries", "cron-person", "v1", "cron-person")
			shim := filepath.Join(root, ".generated", "bin", "cron-person")
			source := filepath.Join(root, "build")
			write := func(path, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(payload, "old")
			current := filepath.Join(root, ".generated", "binaries", "cron-person", "current")
			if err := os.Symlink("v1", current); err != nil {
				t.Fatal(err)
			}
			text := "TOOL_EXECUTABLE=\"" + filepath.Join(current, "cron-person") + "\"\n"
			write(shim, text)
			write(source, "new")
			switch name {
			case "missing shim":
				if err := os.Remove(shim); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				write(shim, text+text)
			case "dynamic":
				write(shim, "TOOL_EXECUTABLE=\"$HOME/payload\"\n")
			case "relative":
				write(shim, "TOOL_EXECUTABLE=\"relative\"\n")
			case "outside":
				write(shim, "TOOL_EXECUTABLE=\""+source+"\"\n")
			case "missing binaries":
				write(shim, "TOOL_EXECUTABLE=\""+source+"\"\n")
				if err := os.RemoveAll(filepath.Join(root, ".generated", "binaries")); err != nil {
					t.Fatal(err)
				}
			case "missing payload":
				if err := os.Remove(payload); err != nil {
					t.Fatal(err)
				}
			case "missing source":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			case "empty":
				write(source, "")
			case "temp blocked":
				write(filepath.Join(root, ".tmp"), "blocked")
			case "rename failure":
				if err := os.Remove(payload); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(payload, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got, err := bootstrap(source, root)
			if name != "success" {
				if err == nil {
					t.Fatal("invalid install succeeded")
				}
				return
			}
			if err != nil || got != payload {
				t.Fatalf("installed=%q err=%v", got, err)
			}
			data, err := os.ReadFile(payload)
			if err != nil || string(data) != "new" {
				t.Fatalf("payload=%q err=%v", data, err)
			}
			info, err := os.Stat(payload)
			if err != nil || info.Mode().Perm() != 0o755 {
				t.Fatalf("mode: %v %v", info, err)
			}
			data, err = os.ReadFile(shim)
			if err != nil || string(data) != text {
				t.Fatalf("shim changed: %q %v", data, err)
			}
			entries, err := os.ReadDir(filepath.Join(root, ".tmp"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary files remain: %v %v", entries, err)
			}
			link, err := os.Readlink(current)
			if err != nil || !strings.HasSuffix(link, "v1") {
				t.Fatalf("current link changed: %q %v", link, err)
			}
			write(filepath.Join(root, "bin", "cron-person"), "through-main")
			t.Chdir(root)
			t.Setenv("dotfiles_dir", root)
			main()
			data, err = os.ReadFile(payload)
			if err != nil || string(data) != "through-main" {
				t.Fatalf("main payload=%q err=%v", data, err)
			}
		})
	}
}

func TestRunErrors(t *testing.T) {
	t.Setenv("dotfiles_dir", "")
	if err := run(); err == nil {
		t.Fatal("missing environment accepted")
	}
	t.Setenv("dotfiles_dir", t.TempDir())
	if err := run(); err == nil {
		t.Fatal("missing shim accepted")
	}
}
