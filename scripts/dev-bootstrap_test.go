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

func TestCoverageGateAndWorkflows(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "coverage.out")
	validProfile := strings.Join([]string{
		"mode: atomic",
		"example.com/pkg/a/file.go:1.1,10.2 9 1",
		"example.com/pkg/a/file.go:11.1,12.2 1 0",
		"example.com/pkg/b/file.go:1.1,20.2 19 2",
		"example.com/pkg/b/file.go:21.1,22.2 1 0",
		"",
	}, "\n")
	if err := os.WriteFile(profilePath, []byte(validProfile), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("coverage_profile", profilePath)
	t.Setenv("coverage_packages", "example.com/pkg/a example.com/pkg/b")
	if err := run(); err != nil {
		t.Fatalf("expected 90%%+ packages to pass, got %v", err)
	}

	// Zero-coverage package missing from profile must fail.
	t.Setenv("coverage_packages", "example.com/pkg/a example.com/pkg/zero")
	if err := run(); err == nil || !strings.Contains(err.Error(), "example.com/pkg/zero") {
		t.Fatalf("expected zero-coverage package to fail, got %v", err)
	}

	// Under-covered package (<90%) must fail.
	underProfilePath := filepath.Join(dir, "under.out")
	underProfile := strings.Join([]string{
		"mode: atomic",
		"example.com/pkg/a/file.go:1.1,10.2 8 1",
		"example.com/pkg/a/file.go:11.1,12.2 2 0",
		"",
	}, "\n")
	if err := os.WriteFile(underProfilePath, []byte(underProfile), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("coverage_profile", underProfilePath)
	t.Setenv("coverage_packages", "example.com/pkg/a")
	if err := run(); err == nil || !strings.Contains(err.Error(), "80.0%") {
		t.Fatalf("expected 80.0%% package to fail, got %v", err)
	}

	// Malformed profile and missing profile file must fail.
	badProfilePath := filepath.Join(dir, "bad.out")
	if err := os.WriteFile(badProfilePath, []byte("mode: atomic\ninvalid-line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("coverage_profile", badProfilePath)
	if err := run(); err == nil {
		t.Fatal("expected malformed coverage profile to fail")
	}
	negProfilePath := filepath.Join(dir, "neg.out")
	if err := os.WriteFile(negProfilePath, []byte("mode: atomic\nexample.com/pkg/a/file.go:1.1,2.2 -1 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("coverage_profile", negProfilePath)
	if err := run(); err == nil {
		t.Fatal("expected negative coverage count to fail")
	}
	t.Setenv("coverage_profile", filepath.Join(dir, "nonexistent.out"))
	if err := run(); err == nil {
		t.Fatal("expected missing coverage profile to fail")
	}
	t.Setenv("coverage_profile", profilePath)
	t.Setenv("coverage_packages", "")
	if err := run(); err == nil {
		t.Fatal("expected empty package list to fail")
	}

	// Verify CI and Release workflows run race detector and package coverage gate.
	for _, wf := range []string{
		filepath.Join("..", ".github", "workflows", "ci.yml"),
		filepath.Join("..", ".github", "workflows", "release.yml"),
	} {
		data, err := os.ReadFile(wf)
		if err != nil {
			t.Fatalf("reading %s: %v", wf, err)
		}
		text := string(data)
		if !strings.Contains(text, "-race") || !strings.Contains(text, "coverage_profile=") {
			t.Fatalf("workflow %s does not enforce -race and coverage_profile gate:\n%s", wf, text)
		}
	}
}
