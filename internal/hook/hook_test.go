package hook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func TestHookScript_Shells(t *testing.T) {
	shells := []string{"bash", "zsh", "fish"}
	for _, shell := range shells {
		script, err := GenerateHookScript(shell, "cron-person")
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", shell, err)
		}
		if !strings.Contains(script, "cron-person") {
			t.Errorf("script for %s missing binary name: %s", shell, script)
		}
	}
}

func TestHookExport_UnauthorizedAndChanged(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "allowed.json")
	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	appDir := filepath.Join(tempDir, "app")
	_ = os.MkdirAll(appDir, 0o755)
	cronrcPath := filepath.Join(appDir, ".cronrc")
	_ = os.WriteFile(cronrcPath, []byte("@hourly ./run.sh\n"), 0o644)

	// 1. Unauthorized
	var stderr bytes.Buffer
	err = HandleHookExport(st, appDir, false, &stderr)
	if err != nil {
		t.Fatalf("unexpected HandleHookExport error: %v", err)
	}
	if !strings.Contains(stderr.String(), "unauthorized") {
		t.Errorf("expected unauthorized notice in stderr, got: %s", stderr.String())
	}

	// 2. Allow
	_, _ = st.Allow(appDir)
	stderr.Reset()
	err = HandleHookExport(st, appDir, false, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected clean output for allowed dir, got: %s", stderr.String())
	}

	// 3. Modified
	_ = os.WriteFile(cronrcPath, []byte("@daily ./run.sh\n"), 0o644)
	stderr.Reset()
	err = HandleHookExport(st, appDir, false, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "modified") && !strings.Contains(stderr.String(), "changed") {
		t.Errorf("expected modified/changed notice in stderr, got: %s", stderr.String())
	}
}
