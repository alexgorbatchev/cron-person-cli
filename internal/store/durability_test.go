package store

import (
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreIgnoresAndRemovesAbandonedStagedWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowed.json")
	projects := map[string]string{}
	for _, name := range []string{"a", "b", "c"} {
		projects[name] = filepath.Join(dir, name)
		if err := os.Mkdir(projects[name], 0o700); err != nil {
			t.Fatal(err)
		}
		writeStoreFile(t, filepath.Join(projects[name], ".cronrc"), "@daily true\n")
	}
	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Allow(projects["a"]); err != nil {
		t.Fatal(err)
	}

	// A writer killed after staging but before renaming, one killed while
	// writing, and files cron-person never stages.
	staged := `[{"dir":"` + projects["b"] + `","path":"` + filepath.Join(projects["b"], ".cronrc") + `","hash":"x"}]`
	abandoned := map[string]string{"allowed.json.123.tmp": staged, "allowed.json.456.tmp": `[{"dir":`}
	unowned := map[string]string{"allowed.json.tmp": "unowned", "allowed.json.abc.tmp": "unowned", "other.json.789.tmp": "unowned"}
	for _, files := range []map[string]string{abandoned, unowned} {
		for name, content := range files {
			writeStoreFile(t, filepath.Join(dir, name), content)
		}
	}

	reopened, err := NewStore(path)
	if err != nil {
		t.Fatalf("abandoned staged writes broke loading: %v", err)
	}
	if got := dirs(mustList(t, reopened)); len(got) != 1 || got[0] != projects["a"] {
		t.Fatalf("records=%v, want only the committed approval", got)
	}

	if _, err := reopened.Allow(projects["c"]); err != nil {
		t.Fatal(err)
	}
	for name := range abandoned {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("abandoned %s survived the next write: %v", name, err)
		}
	}
	for name, content := range unowned {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(data) != content {
			t.Fatalf("unowned %s changed: %q %v", name, data, err)
		}
	}
}

// TestStoreSurvivesKilledWriters kills writer processes at arbitrary points
// of a write and requires the store to stay loadable with only committed
// records. It cannot simulate power loss; the file and directory flushes
// cover that case.
func TestStoreSurvivesKilledWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "allowed.json")
	project := filepath.Join(dir, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeStoreFile(t, filepath.Join(project, ".cronrc"), "@daily true\n")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	for round := range 15 {
		writer := exec.Command(exe, "-test.run=^TestHelperStoreWriter$")
		writer.Env = append(os.Environ(), "STORE_WRITER_HELPER=1", "STORE_WRITER_PATH="+path, "STORE_WRITER_PROJECT="+project)
		if err := writer.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Duration(5+rand.IntN(40)) * time.Millisecond)
		if err := writer.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = writer.Wait() // Killed on purpose.

		st, err := NewStore(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("round %d: store unreadable after killed writer: %v", round, err)
		}
		if got := dirs(mustList(t, st)); len(got) > 1 || (len(got) == 1 && got[0] != project) {
			t.Fatalf("round %d: records=%v", round, got)
		}
	}
}

func TestHelperStoreWriter(t *testing.T) {
	if os.Getenv("STORE_WRITER_HELPER") != "1" {
		return
	}
	st, err := NewStore(os.Getenv("STORE_WRITER_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	project := os.Getenv("STORE_WRITER_PROJECT")
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		if _, err := st.Allow(project); err != nil {
			t.Fatal(err)
		}
		if err := st.Deny(project); err != nil {
			t.Fatal(err)
		}
	}
}

func dirs(records []Record) []string {
	out := make([]string, 0, len(records))
	for _, rec := range records {
		out = append(out, rec.Dir)
	}
	return out
}
