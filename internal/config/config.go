// Package config loads user settings from cron-person's configuration file.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

const (
	// DefaultRetentionDays is how long finished runs are kept by default.
	DefaultRetentionDays = 30
	// DefaultMaxSizeMB is the default target size of all stored run history.
	DefaultMaxSizeMB = 100
	// DefaultMaxRunMB is the default cap on the output stored for one run.
	DefaultMaxRunMB = 10
)

// Config is the contents of config.json. Omitted settings keep their defaults.
type Config struct {
	History History `json:"history"`
}

// History configures run history retention. Zero disables a limit.
type History struct {
	RetentionDays int `json:"retention_days"`
	MaxSizeMB     int `json:"max_size_mb"`
	MaxRunMB      int `json:"max_run_mb"`
}

// Default returns the configuration used when no file exists.
func Default() Config {
	return Config{History: History{RetentionDays: DefaultRetentionDays, MaxSizeMB: DefaultMaxSizeMB, MaxRunMB: DefaultMaxRunMB}}
}

// DefaultPath returns the standard XDG configuration file path for cron-person.
func DefaultPath() (string, error) {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(configHome) {
		return filepath.Join(configHome, "cron-person", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home directory: %w", err)
	}
	return filepath.Join(home, ".config", "cron-person", "config.json"), nil
}

// Load reads the configuration at path, returning defaults when it does not
// exist. Unknown keys are rejected so a misspelled setting cannot be silently
// ignored.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading config %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("parsing config %s: unexpected data after the configuration object", path)
	}
	if cfg.History.RetentionDays < 0 || cfg.History.MaxSizeMB < 0 || cfg.History.MaxRunMB < 0 {
		return Config{}, fmt.Errorf("invalid config %s: history.retention_days, history.max_size_mb, and history.max_run_mb must be zero or greater", path)
	}
	return cfg, nil
}

// MaxOutputBytes is the per-run output cap in bytes; zero means unlimited.
func (h History) MaxOutputBytes() int64 {
	return int64(h.MaxRunMB) << 20
}

// Retention converts the settings into the limits enforced on run history.
func (h History) Retention() history.Retention {
	return history.Retention{
		MaxAge:   time.Duration(h.RetentionDays) * 24 * time.Hour,
		MaxBytes: int64(h.MaxSizeMB) << 20,
	}
}
