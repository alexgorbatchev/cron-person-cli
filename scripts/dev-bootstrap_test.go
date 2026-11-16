package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeBootstrapFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrap(t *testing.T) {
	for _, name := range []string{"success", "no release", "old CLI", "help failure", "missing CLI", "registration failure", "which failure", "missing source", "empty source", "version failure"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dotfiles := filepath.Join(root, "dotfiles with spaces")
			source := filepath.Join(root, "bin", "cron-person")
			release := filepath.Join(dotfiles, ".generated", "binaries", "cron-person", "v1", "cron-person")
			destination := filepath.Join(dotfiles, ".state", "custom-dev-tools", "cron-person")
			commands := filepath.Join(root, "commands")
			writeBootstrapFile(t, release, "managed release", 0o755)
			writeBootstrapFile(t, source, "#!/bin/sh\nprintf '999.0.0-dev.test\\n'\n", 0o755)
			writeBootstrapFile(t, filepath.Join(commands, "dotfiles"), `#!/bin/sh
set -eu
if [ "$4" = "--help" ]; then
  if [ -f help-failure ]; then exit 1; fi
  cat help
elif [ "$2" = dev ]; then
  if [ -f registration-failure ]; then echo "registration refused" >&2; exit 1; fi
  printf '%s\n' "$@" > invocation
  source=${6#cron-person=}
  mkdir -p .state/custom-dev-tools
  cp "$source" .state/custom-dev-tools/cron-person
elif [ "$2" = which ]; then
  if [ -f which-failure ]; then echo "which failed" >&2; exit 1; fi
  printf '%s/.state/custom-dev-tools/cron-person\n' "$PWD"
else
  exit 1
fi
`, 0o755)
			writeBootstrapFile(t, filepath.Join(dotfiles, "help"), "tool dev use --bin name=source", 0o600)
			t.Chdir(root)
			t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
			switch name {
			case "no release":
				if err := os.RemoveAll(filepath.Join(dotfiles, ".generated")); err != nil {
					t.Fatal(err)
				}
			case "old CLI":
				writeBootstrapFile(t, filepath.Join(dotfiles, "help"), "tool install", 0o600)
			case "help failure":
				writeBootstrapFile(t, filepath.Join(dotfiles, "help-failure"), "", 0o600)
			case "missing CLI":
				t.Setenv("PATH", filepath.Join(root, "missing"))
			case "registration failure":
				writeBootstrapFile(t, filepath.Join(dotfiles, "registration-failure"), "", 0o600)
			case "which failure":
				writeBootstrapFile(t, filepath.Join(dotfiles, "which-failure"), "", 0o600)
			case "missing source":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			case "empty source":
				writeBootstrapFile(t, source, "", 0o755)
			case "version failure":
				writeBootstrapFile(t, source, "#!/bin/sh\necho version-failed >&2\nexit 1\n", 0o755)
			}
			got, err := bootstrap(source, dotfiles)
			if name != "success" && name != "no release" {
				if err == nil {
					t.Fatal("invalid bootstrap succeeded")
				}
				if (name == "old CLI" || name == "help failure" || name == "missing CLI") && !strings.Contains(err.Error(), "update dotfiles") {
					t.Fatal(err)
				}
				if name == "registration failure" && !strings.Contains(err.Error(), "registration refused") {
					t.Fatal(err)
				}
			} else {
				if err != nil || got != destination {
					t.Fatalf("path=%q err=%v, want %q", got, err, destination)
				}
				args, readErr := os.ReadFile(filepath.Join(dotfiles, "invocation"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				want := []string{"tool", "dev", "use", "cron-person", "--bin", "cron-person=" + source, "--source-repo", root, "--version", "999.0.0-dev.test"}
				if actual := strings.Split(strings.TrimSpace(string(args)), "\n"); !reflect.DeepEqual(actual, want) {
					t.Fatalf("args=%q want %q", actual, want)
				}
				data, readErr := os.ReadFile(destination)
				if readErr != nil || !strings.Contains(string(data), "999.0.0-dev.test") {
					t.Fatalf("copied=%q err=%v", data, readErr)
				}
				t.Setenv("dotfiles_dir", dotfiles)
				main()
			}
			if name != "no release" {
				data, readErr := os.ReadFile(release)
				if readErr != nil || string(data) != "managed release" {
					t.Fatalf("release changed: %q %v", data, readErr)
				}
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
		t.Fatal("missing build accepted")
	}
}

func TestBootstrapWithoutWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "removed")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap("bin/cron-person", "relative-dotfiles"); err == nil || !strings.Contains(err.Error(), "resolving dotfiles directory") {
		t.Fatalf("missing working directory error = %v", err)
	}
}
