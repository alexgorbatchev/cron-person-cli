package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectAllReportsEveryTrackedDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowed.json")
	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	projects := map[string]string{}
	for _, name := range []string{"allowed", "changed", "orphan", "unreadable"} {
		projects[name] = filepath.Join(dir, name)
		if err := os.Mkdir(projects[name], 0o700); err != nil {
			t.Fatal(err)
		}
		writeStoreFile(t, filepath.Join(projects[name], ".cronrc"), "@daily "+name+"\n")
		if _, err := st.Allow(projects[name]); err != nil {
			t.Fatal(err)
		}
	}
	writeStoreFile(t, filepath.Join(projects["changed"], ".cronrc"), "@daily edited\n")
	if err := os.Remove(filepath.Join(projects["orphan"], ".cronrc")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(projects["unreadable"], ".cronrc"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(projects["unreadable"], ".cronrc"), 0o600) })

	inspections, err := st.InspectAll()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Inspection{}
	for _, in := range inspections {
		got[filepath.Base(in.Record.Dir)] = in
	}
	if in := got["allowed"]; in.Status != StatusAllowed || string(in.Snapshot) != "@daily allowed\n" || in.Err != nil {
		t.Fatalf("allowed: %+v", in)
	}
	if in := got["changed"]; in.Status != StatusChanged || string(in.Snapshot) != "@daily edited\n" {
		t.Fatalf("changed: %+v", in)
	}
	if in := got["orphan"]; in.Status != StatusOrphan || in.Snapshot != nil {
		t.Fatalf("orphan: %+v", in)
	}
	if in := got["unreadable"]; in.Err == nil || !strings.Contains(in.Err.Error(), "checking .cronrc") {
		t.Fatalf("unreadable: %+v, want its read error", in)
	}
	if len(inspections) != 4 || inspections[0].Record.Dir > inspections[3].Record.Dir {
		t.Fatalf("want all four directories in directory order, got %d", len(inspections))
	}

	writeStoreFile(t, path, "{")
	if _, err := st.InspectAll(); err == nil {
		t.Fatal("corrupt store accepted")
	}
}
