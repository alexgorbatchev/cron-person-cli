package display

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)
	for _, tt := range []struct{ path, want string }{
		{home, "~"},
		{filepath.Join(home, "project", ".cronrc"), filepath.Join("~", "project", ".cronrc")},
		{home + "-other/project", home + "-other/project"},
		{"/outside/.cronrc", "/outside/.cronrc"},
	} {
		if got := Path(tt.path); got != tt.want {
			t.Errorf("Path(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
	t.Setenv("HOME", "")
	if got := Path(home); got != home {
		t.Errorf("Path without HOME = %q, want %q", got, home)
	}
	t.Setenv("HOME", "/")
	if got := Path("/project"); got != "~/project" {
		t.Errorf("Path with root HOME = %q", got)
	}
}

func TestError(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)
	_, err := os.Open(filepath.Join(home, "missing file"))
	if err == nil {
		t.Fatal("expected a missing-file error")
	}
	wrapped := fmt.Errorf("loading store from %s: %w", home, err)
	got := Error(wrapped)
	if strings.Contains(got, home) || !strings.Contains(got, "loading store from ~: open ~/missing file:") {
		t.Fatalf("error paths were not folded: %s", got)
	}
	for _, tt := range []struct{ message, want string }{
		{home, "~"},
		{"path='" + home + "/a'", "path='~/a'"},
		{"(" + home + ") [" + home + "/b]", "(~) [~/b]"},
		{home + "-other/a", home + "-other/a"},
		{"/prefix" + home + "/a", "/prefix" + home + "/a"},
		{"outside path", "outside path"},
	} {
		if got := Error(errors.New(tt.message)); got != tt.want {
			t.Errorf("Error(%q) = %q, want %q", tt.message, got, tt.want)
		}
	}
	t.Setenv("HOME", "")
	if got := Error(wrapped); got != wrapped.Error() {
		t.Errorf("error changed without HOME: %s", got)
	}
}
