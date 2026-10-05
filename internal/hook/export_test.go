package hook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func TestHookScriptBinaryAndShellSelection(t *testing.T) {
	for _, tt := range []struct{ shell, binary, want string }{
		{"bash", "", `"cron-person" hook export bash`},
		{"zsh", "/opt/custom-tool", `"/opt/custom-tool" hook export zsh`},
		{"fish", "/opt/custom-tool", `"/opt/custom-tool" hook export fish`},
		{"unsupported", "", ""},
	} {
		t.Run(tt.shell, func(t *testing.T) {
			script, err := GenerateHookScript(tt.shell, tt.binary)
			if tt.want == "" {
				if err == nil || !strings.Contains(err.Error(), "unsupported shell") || script != "" {
					t.Fatalf("script=%q error=%v", script, err)
				}
			} else if err != nil || !strings.Contains(script, tt.want) {
				t.Fatalf("script=%q error=%v, want %q", script, err, tt.want)
			}
		})
	}
}

func TestHookExportAgentAndAbsentCronrc(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := HandleHookExport(st, dir, true, &output); err != nil || output.Len() != 0 {
		t.Fatalf("absent cronrc output=%q error=%v", output.String(), err)
	}
	path := filepath.Join(dir, ".cronrc")
	if err := os.WriteFile(path, []byte("@daily true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := HandleHookExport(st, dir, true, &output); err != nil || output.String() != "WARN: unauthorized .cronrc at "+path+"\n" {
		t.Fatalf("unauthorized output=%q error=%v", output.String(), err)
	}
	if _, err := st.Allow(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("@hourly true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := HandleHookExport(st, dir, true, &output); err != nil || output.String() != "WARN: modified .cronrc at "+path+"\n" {
		t.Fatalf("changed output=%q error=%v", output.String(), err)
	}
}

func TestHookExportUnavailableWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := HandleHookExport(st, ".", false, &output); err == nil {
		t.Fatal("expected working directory resolution error")
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected warning after resolution failure: %q", output.String())
	}
}
