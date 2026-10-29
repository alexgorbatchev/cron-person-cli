package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func mustList(t *testing.T, st *Store) []Record {
	t.Helper()
	records, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func TestReadMethodsReportCorruptReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowed.json")
	writeStoreFile(t, filepath.Join(dir, ".cronrc"), "@daily true\n")
	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Allow(dir); err != nil {
		t.Fatal(err)
	}
	writeStoreFile(t, path, "corrupt json")
	for _, read := range []func() error{
		func() error { _, err := st.List(); return err },
		func() error { _, _, err := st.Get(dir); return err },
		func() error { _, _, err := st.Status(dir); return err },
		func() error { _, _, _, err := st.Inspect(dir); return err },
	} {
		if err := read(); err == nil || !strings.Contains(err.Error(), "loading store") {
			t.Fatalf("expected reload error, got %v", err)
		}
	}
}
