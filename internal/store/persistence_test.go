package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeStoreFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultStorePath(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		name, stateHome, home, want, wantErr string
	}{
		{"xdg", filepath.Join(dir, "state"), "", filepath.Join(dir, "state", "cron-person", "allowed.json"), ""},
		{"home fallback", "", dir, filepath.Join(dir, ".local", "state", "cron-person", "allowed.json"), ""},
		{"missing home", "", "", "", "resolving user home directory"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", tt.stateHome)
			t.Setenv("HOME", tt.home)
			got, err := DefaultStorePath()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("path=%q error=%v", got, err)
				}
			} else if err != nil || got != tt.want {
				t.Fatalf("path=%q error=%v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestStoreLoadErrors(t *testing.T) {
	for _, tt := range []struct{ name, content string }{
		{"truncated json", "["},
		{"wrong shape", "{}"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			writeStoreFile(t, path, tt.content)
			if _, err := NewStore(path); err == nil || !strings.Contains(err.Error(), "loading store") {
				t.Fatalf("expected loading error, got %v", err)
			}
		})
	}
	if _, err := NewStore(t.TempDir()); err == nil {
		t.Fatal("expected error reading a directory as a store")
	}
}

func TestStoreCopiesAndSortedPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, name := range []string{"z", "a"} {
		project := filepath.Join(dir, name)
		if err := os.Mkdir(project, 0o700); err != nil {
			t.Fatal(err)
		}
		writeStoreFile(t, filepath.Join(project, ".cronrc"), "@daily true\n")
		if _, err := st.Allow(project); err != nil {
			t.Fatal(err)
		}
	}
	want = []string{filepath.Join(dir, "a"), filepath.Join(dir, "z")}
	t.Chdir(dir)
	rec, ok := st.Get("a")
	if !ok || rec.Dir != want[0] || rec.Hash == "" || rec.AllowedAt.IsZero() {
		t.Fatalf("invalid retrieved record: %+v, found=%v", rec, ok)
	}
	rec.Hash = "changed copy"
	list := st.List()
	list[0].Hash = "changed list"
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []*Store{st, reloaded} {
		records := current.List()
		if got := []string{records[0].Dir, records[1].Dir}; !reflect.DeepEqual(got, want) {
			t.Fatalf("order=%v, want %v", got, want)
		}
		if got, ok := current.Get("a"); !ok || got.Hash == rec.Hash || got.Hash == list[0].Hash {
			t.Fatalf("record was modified through returned copy: %+v", got)
		}
		if got, ok := current.Get("missing"); ok || got != nil {
			t.Fatalf("unexpected missing record: %+v, %v", got, ok)
		}
	}
	if pruned, err := st.Prune(); err != nil || len(pruned) != 0 {
		t.Fatalf("valid projects pruned: %v, %v", pruned, err)
	}
}

func TestStorePersistenceErrors(t *testing.T) {
	for _, failure := range []string{"parent is file", "temp is directory", "destination is directory"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state", "allowed.json")
			st, err := NewStore(path)
			if err != nil {
				t.Fatal(err)
			}
			project := filepath.Join(dir, "project")
			if err := os.Mkdir(project, 0o700); err != nil {
				t.Fatal(err)
			}
			writeStoreFile(t, filepath.Join(project, ".cronrc"), "@daily true\n")
			want := "creating state directory"
			switch failure {
			case "parent is file":
				writeStoreFile(t, filepath.Dir(path), "blocked")
			case "temp is directory":
				if err := os.MkdirAll(path+".tmp", 0o700); err != nil {
					t.Fatal(err)
				}
				want = "writing store temp file"
			case "destination is directory":
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
				want = "committing store file"
			}
			if _, err := st.Allow(project); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("allow error=%v, want %q", err, want)
			}
			if err := st.Deny(project); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("deny error=%v, want %q", err, want)
			}
		})
	}
}

func TestStoreInvalidCronrcAndPruneFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	cronrcPath := filepath.Join(dir, ".cronrc")
	writeStoreFile(t, cronrcPath, "invalid schedule\n")
	if _, err := st.Allow(dir); err == nil || !strings.Contains(err.Error(), "validating .cronrc") {
		t.Fatalf("expected validation error, got %v", err)
	}
	writeStoreFile(t, cronrcPath, "@daily true\n")
	if _, err := st.Allow(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cronrcPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cronrcPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if status, _, err := st.Status(dir); err != nil || status != StatusMissingCronrc {
		t.Fatalf("directory .cronrc status=%s error=%v", status, err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Prune(); err == nil || !strings.Contains(err.Error(), "writing store temp file") {
		t.Fatalf("expected pruning persistence error, got %v", err)
	}
}

func TestStoreUnavailableWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(dir, "removed")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	if err := os.Remove(cwd); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Allow("relative"); err == nil {
		t.Fatal("allow succeeded without a working directory")
	}
	if err := st.Deny("relative"); err == nil {
		t.Fatal("deny succeeded without a working directory")
	}
	if _, _, err := st.Status("relative"); err == nil {
		t.Fatal("status succeeded without a working directory")
	}
	if rec, ok := st.Get("relative"); ok || rec != nil {
		t.Fatalf("unexpected record: %+v, %v", rec, ok)
	}
}
