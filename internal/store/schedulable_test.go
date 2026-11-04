package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
)

func TestSchedulableSplitsReadyAndSkippedDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowed.json")
	var records []Record
	for _, name := range []string{"ready", "changed", "orphan", "special", "malformed", "noncanonical", "removed-noncanonical", "line\nbreak", "loop"} {
		project := filepath.Join(dir, name)
		if err := os.Mkdir(project, 0o700); err != nil {
			t.Fatal(err)
		}
		content := "@daily " + name + "\n"
		if name == "malformed" {
			// Approved content the parser rejects, as after a grammar change.
			content = "not a schedule\n"
		}
		cronrcPath := filepath.Join(project, ".cronrc")
		writeStoreFile(t, cronrcPath, content)
		key := project
		if strings.HasSuffix(name, "noncanonical") {
			key = project + "/"
		}
		records = append(records, Record{Dir: key, Path: cronrcPath, Hash: cronrc.ComputeHash([]byte(content))})
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	writeStoreFile(t, path, string(data))
	writeStoreFile(t, filepath.Join(dir, "changed", ".cronrc"), "@daily edited\n")
	for _, name := range []string{"orphan", "removed-noncanonical"} {
		if err := os.Remove(filepath.Join(dir, name, ".cronrc")); err != nil {
			t.Fatal(err)
		}
	}
	loop := filepath.Join(dir, "loop", ".cronrc")
	if err := os.Remove(loop); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".cronrc", loop); err != nil {
		t.Fatal(err)
	}
	special := filepath.Join(dir, "special", ".cronrc")
	if err := os.Remove(special); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(special, 0o700); err != nil {
		t.Fatal(err)
	}

	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ready, skipped, err := st.Schedulable()
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || filepath.Base(ready[0].Record.Dir) != "ready" || len(ready[0].File.Tasks) != 1 || ready[0].File.Tasks[0].Command != "ready" {
		t.Fatalf("ready = %+v", ready)
	}
	want := map[string]string{
		"changed":               ".cronrc changed since it was allowed: run 'cron-person allow " + filepath.Join(dir, "changed") + "'",
		"orphan":                ".cronrc no longer exists",
		"special":               ".cronrc is not a regular file",
		"malformed":             "parsing " + filepath.Join(dir, "malformed", ".cronrc") + ": line 1:",
		"noncanonical/":         "is not the canonical path " + filepath.Join(dir, "noncanonical") + ": run 'cron-person deny",
		"removed-noncanonical/": "is not the canonical path " + filepath.Join(dir, "removed-noncanonical"),
		"line\nbreak":           "directory path contains a line break or NUL",
		"loop":                  "checking .cronrc",
	}
	if len(skipped) != len(want) {
		t.Fatalf("skipped %d directories, want %d: %+v", len(skipped), len(want), skipped)
	}
	wantStatus := map[string]DirStatus{
		"changed": StatusChanged, "orphan": StatusOrphan, "special": StatusMissingCronrc,
		"malformed": StatusInvalid, "noncanonical/": StatusUnauthorized, "removed-noncanonical/": StatusUnauthorized,
		"line\nbreak": StatusInvalid, "loop": StatusUnreadable,
	}
	for _, s := range skipped {
		name := strings.TrimPrefix(s.Record.Dir, dir+string(filepath.Separator))
		if w, ok := want[name]; !ok || !strings.Contains(s.Problem.Error(), w) {
			t.Errorf("%s: problem %v, want %q", name, s.Problem, w)
		}
		if s.Status != wantStatus[name] {
			t.Errorf("%s: status %s, want %s", name, s.Status, wantStatus[name])
		}
	}

	// The remedy the warning names removes the entry it warns about.
	if err := st.Deny(filepath.Join(dir, "noncanonical")); err != nil {
		t.Fatal(err)
	}
	_, skipped, err = st.Schedulable()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skipped {
		if s.Record.Dir == filepath.Join(dir, "noncanonical")+"/" {
			t.Fatalf("deny left the non-canonical entry: %+v", s)
		}
	}

	writeStoreFile(t, path, "{")
	if _, _, err := st.Schedulable(); err == nil {
		t.Fatal("corrupt store accepted")
	}
}

func TestAllowRefusesUnschedulableDirectoryPaths(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(filepath.Join(dir, "allowed.json"))
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(dir, "line\nbreak")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeStoreFile(t, filepath.Join(project, ".cronrc"), "@daily true\n")
	if _, err := st.Allow(project); err == nil || !strings.Contains(err.Error(), "line break or NUL") {
		t.Fatalf("Allow = %v, want a refusal", err)
	}
}
