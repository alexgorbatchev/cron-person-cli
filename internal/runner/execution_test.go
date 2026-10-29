package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func runnerProject(t *testing.T, content string) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".cronrc"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Allow(dir); err != nil {
		t.Fatal(err)
	}
	return st, dir
}

func TestRunnerEnvironmentAndStreams(t *testing.T) {
	for _, tt := range []struct{ name, shell, shellAssignment string }{
		{"default shell", "", ""},
		{"inherited shell", "/bin/sh", ""},
		{"cronrc shell overrides inherited", "/does-not-exist", "SHELL=/bin/sh\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHELL", tt.shell)
			t.Setenv("RUNNER_VALUE", "inherited")
			t.Setenv("RUNNER_INHERITED", "kept")
			st, dir := runnerProject(t, tt.shellAssignment+"RUNNER_VALUE=file\n@daily true\n")
			var stdout, stderr bytes.Buffer
			opts := ExecOptions{
				Dir:     dir,
				Command: `read input; printf '%s\n' "$RUNNER_VALUE" "$RUNNER_INHERITED" "$input" "$PWD" "$CRON_PERSON_DIR" "$CRON_PERSON_CRONRC"; printf 'diagnostic' >&2`,
				Stdin:   strings.NewReader("provided input\n"), Stdout: &stdout, Stderr: &stderr,
				ExtraEnv: map[string]string{"RUNNER_VALUE": "override"},
			}
			if err := NewRunner(st).Exec(t.Context(), opts); err != nil {
				t.Fatal(err)
			}
			want := strings.Join([]string{"override", "kept", "provided input", dir, dir, filepath.Join(dir, ".cronrc"), ""}, "\n")
			if stdout.String() != want || stderr.String() != "diagnostic" {
				t.Fatalf("stdout=%q stderr=%q, want stdout=%q", stdout.String(), stderr.String(), want)
			}
		})
	}
}

func TestRunnerExecutionFailures(t *testing.T) {
	for _, tt := range []struct {
		name, command, shell string
		cancelled            bool
	}{
		{"command failure", "printf failed >&2; exit 7", "/bin/sh", false},
		{"missing executable", "true", "/does-not-exist", false},
		{"cancelled context", "true", "/bin/sh", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHELL", tt.shell)
			st, dir := runnerProject(t, "@daily true\n")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			var stderr bytes.Buffer
			err := NewRunner(st).Exec(ctx, ExecOptions{Dir: dir, Command: tt.command, Stderr: &stderr})
			if err == nil || !strings.Contains(err.Error(), "task failed in ") {
				t.Fatalf("expected execution failure, got %v", err)
			}
			if tt.cancelled && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation cause: %v", err)
			}
			if tt.name == "command failure" {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 || stderr.String() != "failed" {
					t.Fatalf("lost exit status or stderr: %v, %q", err, stderr.String())
				}
			}
		})
	}
}

func TestRunnerMissingAndUnusableCronrc(t *testing.T) {
	for _, tt := range []struct{ name, want string }{
		{"missing", "no .cronrc file found"},
		{"orphan", "does not exist on disk"},
		{"stat failure", "checking authorization"},
		{"invalid stored schedule", "parsing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st, dir := runnerProject(t, "@daily true\n")
			path := filepath.Join(dir, ".cronrc")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			switch tt.name {
			case "missing":
				if err := st.Deny(dir); err != nil {
					t.Fatal(err)
				}
			case "stat failure":
				if err := os.Symlink(".cronrc", path); err != nil {
					t.Fatal(err)
				}
			case "invalid stored schedule":
				content := []byte("invalid schedule\n")
				if err := os.WriteFile(path, content, 0o600); err != nil {
					t.Fatal(err)
				}
				// Exercise a corrupt persisted authorization, not an alternate runner implementation.
				data, err := json.Marshal([]store.Record{{Dir: dir, Path: path, Hash: cronrc.ComputeHash(content)}})
				if err != nil {
					t.Fatal(err)
				}
				statePath := filepath.Join(dir, "state.json")
				if err := os.WriteFile(statePath, data, 0o600); err != nil {
					t.Fatal(err)
				}
				st, err = store.NewStore(statePath)
				if err != nil {
					t.Fatal(err)
				}
			}
			marker := filepath.Join(dir, "must-not-run")
			err := NewRunner(st).Exec(t.Context(), ExecOptions{Dir: dir, Command: "touch must-not-run"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("blocked command ran: %v", err)
			}
		})
	}
}

func TestRunnerUnavailableWorkingDirectory(t *testing.T) {
	st, dir := runnerProject(t, "@daily true\n")
	cwd := filepath.Join(dir, "removed")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	if err := os.Remove(cwd); err != nil {
		t.Fatal(err)
	}
	err := NewRunner(st).Exec(t.Context(), ExecOptions{Dir: "relative", Command: "true"})
	if err == nil || !strings.Contains(err.Error(), "resolving target directory") {
		t.Fatalf("expected directory resolution error, got %v", err)
	}
}

func TestRunnerRejectsDirectoryAfterFailedStoreAllow(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state", "allowed.json")
	st, err := store.NewStore(statePath)
	if err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(dir, "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".cronrc"), []byte("@daily true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Block committing statePath by turning it into a directory.
	if err := os.MkdirAll(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Allow(projectDir); err == nil {
		t.Fatal("expected Allow to fail when statePath is a directory")
	}

	marker := filepath.Join(projectDir, "ran")
	err = NewRunner(st).Exec(t.Context(), ExecOptions{
		Dir:     projectDir,
		Command: "touch ran",
	})
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("expected unauthorized error after failed Allow, got %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("command executed after failed Allow: %v", statErr)
	}
}
