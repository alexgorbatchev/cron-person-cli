package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

func TestDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tt := range []struct{ xdg, want string }{
		{"/xdg/config", "/xdg/config/cron-person/config.json"},
		{"relative/config", filepath.Join(home, ".config", "cron-person", "config.json")},
		{"", filepath.Join(home, ".config", "cron-person", "config.json")},
	} {
		t.Setenv("XDG_CONFIG_HOME", tt.xdg)
		if got, err := DefaultPath(); err != nil || got != tt.want {
			t.Fatalf("XDG_CONFIG_HOME=%q: DefaultPath = %q, %v; want %q", tt.xdg, got, err, tt.want)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := DefaultPath(); err == nil {
		t.Fatal("expected an error without a home directory")
	}
}

func TestLoad(t *testing.T) {
	day := 24 * time.Hour
	for _, tt := range []struct {
		name, content string
		want          history.Retention
		wantErr       string
	}{
		{"missing file uses defaults", "", history.Retention{MaxAge: 30 * day, MaxBytes: 100 << 20}, ""},
		{"negative run limit", `{"history":{"max_run_mb":-1}}`, history.Retention{}, "history.max_run_mb"},
		{"partial settings keep other defaults", `{"history":{"retention_days":7}}`, history.Retention{MaxAge: 7 * day, MaxBytes: 100 << 20}, ""},
		{"zero disables limits", `{"history":{"retention_days":0,"max_size_mb":0}}`, history.Retention{}, ""},
		{"size only", `{"history":{"retention_days":0,"max_size_mb":5}}`, history.Retention{MaxBytes: 5 << 20}, ""},
		{"unknown key", `{"history":{"retention_day":7}}`, history.Retention{}, `unknown field "retention_day"`},
		{"negative value", `{"history":{"max_size_mb":-1}}`, history.Retention{}, "must be zero or greater"},
		{"malformed json", `{"history":`, history.Retention{}, "parsing config"},
		{"trailing data", `{} {}`, history.Retention{}, "unexpected data"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if tt.content != "" {
				if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), path) {
					t.Fatalf("Load error = %v, want %q naming the file", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.History.Retention(); got != tt.want {
				t.Fatalf("Retention = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadReportsUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "reading config") {
		t.Fatalf("Load(directory) error = %v", err)
	}
}

func TestMaxOutputBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, tt := range []struct {
		content string
		want    int64
	}{
		{"", 10 << 20},
		{`{"history":{"max_run_mb":2}}`, 2 << 20},
		{`{"history":{"max_run_mb":0}}`, 0},
	} {
		if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
			t.Fatal(err)
		}
		if tt.content == "" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		cfg, err := Load(path)
		if err != nil || cfg.History.MaxOutputBytes() != tt.want {
			t.Fatalf("%q: MaxOutputBytes = %d, %v; want %d", tt.content, cfg.History.MaxOutputBytes(), err, tt.want)
		}
	}
}
