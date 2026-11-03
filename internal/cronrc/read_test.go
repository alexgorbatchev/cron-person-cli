package cronrc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileBounds(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, size int) string {
		t.Helper()
		path := filepath.Join(dir, name)
		line := "@daily true\n"
		data := strings.Repeat(line, size/len(line)) + strings.Repeat("#", size%len(line))
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if data, err := ReadFile(write("at-limit", MaxSize)); err != nil || len(data) != MaxSize {
		t.Fatalf("file at the limit: %d bytes, %v", len(data), err)
	}
	if _, err := ReadFile(write("over-limit", MaxSize+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("file over the limit: %v, want ErrTooLarge", err)
	}
	if _, err := ParseFile(write("parse-over-limit", MaxSize+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("ParseFile over the limit: %v, want ErrTooLarge", err)
	}
	if _, err := ReadFile(dir); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("directory: %v, want ErrNotRegular", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(write("target", 12), link); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadFile(link); err != nil || string(data) != "@daily true\n" {
		t.Fatalf("symlink to a regular file: %q, %v", data, err)
	}
	if _, err := ReadFile(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v, want not-exist", err)
	}
}

// BenchmarkParseFileAtLimit measures the largest .cronrc ReadFile accepts:
// 61,680 schedule lines in 1 MiB.
func BenchmarkParseFileAtLimit(b *testing.B) {
	path := filepath.Join(b.TempDir(), ".cronrc")
	line := "*/5 * * * * true\n"
	data := strings.Repeat(line, MaxSize/len(line))
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := ParseFile(path); err != nil {
			b.Fatal(err)
		}
	}
}
