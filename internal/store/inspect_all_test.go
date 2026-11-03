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
	names := []string{"allowed", "changed", "orphan", "unreadable"}
	// Root reads files regardless of permissions, so it cannot have an
	// unreadable .cronrc.
	unreadable := os.Geteuid() != 0
	if !unreadable {
		names = names[:3]
	}
	projects := map[string]string{}
	for _, name := range names {
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
	if unreadable {
		if err := os.Chmod(filepath.Join(projects["unreadable"], ".cronrc"), 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(projects["unreadable"], ".cronrc"), 0o600) })
	}

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
	if in := got["unreadable"]; unreadable && (in.Err == nil || !strings.Contains(in.Err.Error(), "checking .cronrc")) {
		t.Fatalf("unreadable: %+v, want its read error", in)
	}
	last := len(names) - 1
	if len(inspections) != len(names) || inspections[0].Record.Dir > inspections[last].Record.Dir {
		t.Fatalf("want all %d directories in directory order, got %d", len(names), len(inspections))
	}

	// A key that is not the canonical absolute path never matches when a task
	// runs, so it must not look authorized here either.
	project := projects["allowed"]
	edited := `[{"dir":"` + project + `/","path":"` + filepath.Join(project, ".cronrc") + `","hash":"` + got["allowed"].Record.Hash + `"}]`
	writeStoreFile(t, path, edited)
	inspections, err = st.InspectAll()
	if err != nil || len(inspections) != 1 || inspections[0].Status != StatusUnauthorized {
		t.Fatalf("non-canonical record: %+v, %v; want unauthorized", inspections, err)
	}
	if status, _, err := st.Status(project + "/"); err != nil || status != inspections[0].Status {
		t.Fatalf("Status disagrees with InspectAll: %s, %v", status, err)
	}

	writeStoreFile(t, path, "{")
	if _, err := st.InspectAll(); err == nil {
		t.Fatal("corrupt store accepted")
	}
}
