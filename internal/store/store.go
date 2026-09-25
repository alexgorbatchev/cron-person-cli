package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
)

// DirStatus represents the authorization status of a directory.
type DirStatus string

const (
	StatusAllowed       DirStatus = "allowed"
	StatusChanged       DirStatus = "changed"
	StatusUnauthorized DirStatus = "unauthorized"
	StatusMissingCronrc DirStatus = "missing_cronrc"
	StatusOrphan        DirStatus = "orphan"
)

// Record holds authorization data for a directory containing a .cronrc file.
type Record struct {
	Dir       string    `json:"dir"`
	Path      string    `json:"path"`
	Hash      string    `json:"hash"`
	AllowedAt time.Time `json:"allowed_at"`
}

// Store manages persistence of authorized .cronrc files.
type Store struct {
	mu       sync.RWMutex
	filePath string
	records  map[string]Record
}

// DefaultStorePath returns the standard XDG state file path for cron-person.
func DefaultStorePath() (string, error) {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		return filepath.Join(stateHome, "cron-person", "allowed.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "cron-person", "allowed.json"), nil
}

// NewStore initializes a Store loaded from filePath.
func NewStore(filePath string) (*Store, error) {
	s := &Store{
		filePath: filePath,
		records:  make(map[string]Record),
	}

	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading store from %s: %w", filePath, err)
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var raw []Record
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	s.records = make(map[string]Record, len(raw))
	for _, rec := range raw {
		s.records[rec.Dir] = rec
	}
	return nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	list := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		list = append(list, rec)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Dir < list[j].Dir
	})

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling store data: %w", err)
	}

	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return fmt.Errorf("writing store temp file: %w", err)
	}

	if err := os.Rename(tmpFile, s.filePath); err != nil {
		return fmt.Errorf("committing store file: %w", err)
	}
	return nil
}

// Allow registers or updates authorization for the .cronrc in dir.
func (s *Store) Allow(dir string) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving directory path: %w", err)
	}

	cronrcPath := filepath.Join(absDir, ".cronrc")
	parsed, err := cronrc.ParseFile(cronrcPath)
	if err != nil {
		return nil, fmt.Errorf("validating .cronrc in %s: %w", absDir, err)
	}

	rec := Record{
		Dir:       absDir,
		Path:      cronrcPath,
		Hash:      parsed.Hash,
		AllowedAt: time.Now().UTC(),
	}

	s.records[absDir] = rec
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return &rec, nil
}

// Deny removes authorization for dir.
func (s *Store) Deny(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving directory path: %w", err)
	}

	delete(s.records, absDir)
	return s.saveLocked()
}

// Get returns the stored record for dir.
func (s *Store) Get(dir string) (*Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}

	rec, ok := s.records[absDir]
	if !ok {
		return nil, false
	}
	return &rec, true
}

// List returns a sorted copy of all stored records.
func (s *Store) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		list = append(list, rec)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Dir < list[j].Dir
	})
	return list
}

// Status inspects disk and compares with store to determine the directory's status.
func (s *Store) Status(dir string) (DirStatus, *Record, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, fmt.Errorf("resolving path: %w", err)
	}

	cronrcPath := filepath.Join(absDir, ".cronrc")
	fileInfo, statErr := os.Stat(cronrcPath)

	s.mu.RLock()
	rec, hasRecord := s.records[absDir]
	s.mu.RUnlock()

	if statErr != nil {
		if os.IsNotExist(statErr) {
			if hasRecord {
				return StatusOrphan, &rec, nil
			}
			return StatusMissingCronrc, nil, nil
		}
		return "", nil, fmt.Errorf("checking .cronrc: %w", statErr)
	}

	if fileInfo.IsDir() {
		return StatusMissingCronrc, nil, nil
	}

	data, err := os.ReadFile(cronrcPath)
	if err != nil {
		return "", nil, fmt.Errorf("reading .cronrc: %w", err)
	}

	currentHash := cronrc.ComputeHash(data)

	if !hasRecord {
		return StatusUnauthorized, nil, nil
	}

	if rec.Hash != currentHash {
		return StatusChanged, &rec, nil
	}

	return StatusAllowed, &rec, nil
}

// Prune removes records for directories that no longer exist or no longer have a .cronrc file.
func (s *Store) Prune() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var pruned []string
	for dir := range s.records {
		cronrcPath := filepath.Join(dir, ".cronrc")
		if info, err := os.Stat(cronrcPath); err != nil || info.IsDir() {
			delete(s.records, dir)
			pruned = append(pruned, dir)
		}
	}

	if len(pruned) > 0 {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	sort.Strings(pruned)
	return pruned, nil
}
