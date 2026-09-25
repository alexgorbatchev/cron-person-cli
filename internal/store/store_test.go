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

func TestStore_PersistenceAndPrune(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "state", "allowed.json")

	st, err := NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	proj1 := filepath.Join(tempDir, "proj1")
	proj2 := filepath.Join(tempDir, "proj2")
	_ = os.MkdirAll(proj1, 0o755)
	_ = os.MkdirAll(proj2, 0o755)
	_ = os.WriteFile(filepath.Join(proj1, ".cronrc"), []byte("@hourly echo 1\n"), 0o644)
	_ = os.WriteFile(filepath.Join(proj2, ".cronrc"), []byte("@daily echo 2\n"), 0o644)

	_, _ = st.Allow(proj1)
	_, _ = st.Allow(proj2)

	// Re-load store from disk
	st2, err := NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(st2.List()) != 2 {
		t.Fatalf("expected 2 records, got %d", len(st2.List()))
	}

	// Delete proj2
	_ = os.RemoveAll(proj2)

	pruned, err := st2.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned) != 1 || pruned[0] != proj2 {
		t.Fatalf("expected pruned [proj2], got %v", pruned)
	}
	if len(st2.List()) != 1 {
		t.Fatalf("expected 1 record after prune, got %d", len(st2.List()))
	}
}
