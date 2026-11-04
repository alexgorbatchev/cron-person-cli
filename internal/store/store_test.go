package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_AllowDenyStatus(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "state", "allowed.json")

	st, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("unexpected NewStore error: %v", err)
	}

	projectDir := filepath.Join(tempDir, "my-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. No .cronrc exists yet
	status, _, err := st.Status(projectDir)
	if err != nil {
		t.Fatalf("unexpected status error: %v", err)
	}
	if status != StatusMissingCronrc {
		t.Fatalf("expected StatusMissingCronrc, got %v", status)
	}

	// 2. Create .cronrc (unauthorized)
	cronrcPath := filepath.Join(projectDir, ".cronrc")
	if err := os.WriteFile(cronrcPath, []byte("@hourly ./task.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, _, err = st.Status(projectDir)
	if err != nil {
		t.Fatalf("unexpected status error: %v", err)
	}
	if status != StatusUnauthorized {
		t.Fatalf("expected StatusUnauthorized, got %v", status)
	}

	// 3. Allow
	rec, err := st.Allow(projectDir)
	if err != nil {
		t.Fatalf("unexpected Allow error: %v", err)
	}
	if rec.Dir != projectDir || rec.Hash == "" {
		t.Fatalf("invalid record: %+v", rec)
	}

	status, rec, err = st.Status(projectDir)
	if err != nil {
		t.Fatalf("unexpected status error: %v", err)
	}
	if status != StatusAllowed {
		t.Fatalf("expected StatusAllowed, got %v", status)
	}

	// 4. Modify .cronrc -> should become StatusChanged / StatusBlocked
	if err := os.WriteFile(cronrcPath, []byte("@hourly ./task.sh\n@daily ./backup.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, _, err = st.Status(projectDir)
	if err != nil {
		t.Fatalf("unexpected status error: %v", err)
	}
	if status != StatusChanged {
		t.Fatalf("expected StatusChanged, got %v", status)
	}

	// 5. Re-allow
	_, err = st.Allow(projectDir)
	if err != nil {
		t.Fatalf("unexpected re-allow error: %v", err)
	}

	status, _, err = st.Status(projectDir)
	if err != nil {
		t.Fatalf("unexpected status error: %v", err)
	}
	if status != StatusAllowed {
		t.Fatalf("expected StatusAllowed after re-allow, got %v", status)
	}

	// 6. Deny
	if err := st.Deny(projectDir); err != nil {
		t.Fatalf("unexpected Deny error: %v", err)
	}

	status, _, err = st.Status(projectDir)
	if err != nil {
		t.Fatalf("unexpected status error: %v", err)
	}
	if status != StatusUnauthorized {
		t.Fatalf("expected StatusUnauthorized after deny, got %v", status)
	}
}

func TestStore_PersistenceAndOrphans(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "state", "allowed.json")

	st, err := NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	proj1 := filepath.Join(tempDir, "proj1")
	proj2 := filepath.Join(tempDir, "proj2")
	for _, proj := range []string{proj1, proj2} {
		if err := os.MkdirAll(proj, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeStoreFile(t, filepath.Join(proj1, ".cronrc"), "@hourly echo 1\n")
	writeStoreFile(t, filepath.Join(proj2, ".cronrc"), "@daily echo 2\n")
	for _, proj := range []string{proj1, proj2} {
		if _, err := st.Allow(proj); err != nil {
			t.Fatal(err)
		}
	}

	// Re-load store from disk
	st2, err := NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(mustList(t, st2)) != 2 {
		t.Fatalf("expected 2 records, got %d", len(mustList(t, st2)))
	}

	// Delete proj2
	if err := os.RemoveAll(proj2); err != nil {
		t.Fatal(err)
	}

	// A removed directory stays tracked as an orphan until it is denied.
	if status, _, err := st2.Status(proj2); err != nil || status != StatusOrphan {
		t.Fatalf("removed directory status=%s err=%v, want %s", status, err, StatusOrphan)
	}
	if err := st2.Deny(proj2); err != nil {
		t.Fatal(err)
	}
	if len(mustList(t, st2)) != 1 {
		t.Fatalf("expected 1 record after deny, got %d", len(mustList(t, st2)))
	}
}

func TestInspectReturnsTheSnapshotItHashed(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(filepath.Join(dir, "state", "allowed.json"))
	if err != nil {
		t.Fatal(err)
	}
	cronrcPath := filepath.Join(dir, ".cronrc")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(cronrcPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range []struct {
		content string
		allow   bool
		want    DirStatus
	}{
		{"@hourly ./a.sh\n", false, StatusUnauthorized},
		{"@hourly ./a.sh\n", true, StatusAllowed},
		{"@hourly ./b.sh\n", false, StatusChanged},
	} {
		write(step.content)
		if step.allow {
			if _, err := st.Allow(dir); err != nil {
				t.Fatal(err)
			}
		}
		status, _, snapshot, err := st.Inspect(dir)
		if err != nil || status != step.want || string(snapshot) != step.content {
			t.Fatalf("Inspect = %s, %q, %v; want %s, %q", status, snapshot, err, step.want, step.content)
		}
	}
	if err := os.Remove(cronrcPath); err != nil {
		t.Fatal(err)
	}
	if status, _, snapshot, err := st.Inspect(dir); err != nil || status != StatusOrphan || snapshot != nil {
		t.Fatalf("Inspect without .cronrc = %s, %q, %v", status, snapshot, err)
	}
}
