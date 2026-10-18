package store

import (
	"encoding/json"
	"errors"
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
	StatusUnauthorized  DirStatus = "unauthorized"
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

// Path returns the authorization file used by this store.
func (s *Store) Path() string { return s.filePath }

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
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolving store path %s: %w", filePath, err)
	}

	s := &Store{
		filePath: absPath,
		records:  make(map[string]Record),
	}

	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading store from %s: %w", absPath, err)
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

func (s *Store) withFileLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	lockPath := s.filePath + ".lock"
	lockF, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("locking store file: %w", err)
	}
	if err := lockFile(lockF); err != nil {
		_ = lockF.Close()
		return fmt.Errorf("locking store file: %w", err)
	}
	defer func() {
		_ = unlockFile(lockF)
		_ = lockF.Close()
	}()

	return fn()
}

func (s *Store) reloadLocked() error {
	info, err := os.Stat(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.records = make(map[string]Record)
			return nil
		}
		return fmt.Errorf("loading store from %s: %w", s.filePath, err)
	}
	if info.IsDir() {
		return nil
	}
	if err := s.load(); err != nil {
		return fmt.Errorf("loading store from %s: %w", s.filePath, err)
	}
	return nil
}

func (s *Store) cloneRecordsLocked() map[string]Record {
	cloned := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		cloned[k] = v
	}
	return cloned
}

func (s *Store) saveRecordsLocked(records map[string]Record) error {
	list := make([]Record, 0, len(records))
	for _, rec := range records {
		list = append(list, rec)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Dir < list[j].Dir
	})

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling store data: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(s.filePath), filepath.Base(s.filePath)+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing store temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	_, writeErr := tmpFile.Write(data)
	closeErr := tmpFile.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing store temp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("committing store file: %w", err)
	}
	s.records = records
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

	err = s.withFileLock(func() error {
		if err := s.reloadLocked(); err != nil {
			return err
		}
		next := s.cloneRecordsLocked()
		next[absDir] = rec
		return s.saveRecordsLocked(next)
	})
	if err != nil {
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

	return s.withFileLock(func() error {
		if err := s.reloadLocked(); err != nil {
			return err
		}
		next := s.cloneRecordsLocked()
		delete(next, absDir)
		return s.saveRecordsLocked(next)
	})
}

// Get returns the stored record for dir.
func (s *Store) Get(dir string) (*Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = s.reloadLocked()

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
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = s.reloadLocked()

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
	status, rec, _, err := s.Inspect(dir)
	return status, rec, err
}

// Inspect reads a single .cronrc snapshot from dir and compares its raw-byte hash against the store.
func (s *Store) Inspect(dir string) (DirStatus, *Record, []byte, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolving path: %w", err)
	}

	cronrcPath := filepath.Join(absDir, ".cronrc")
	fileInfo, statErr := os.Stat(cronrcPath)

	s.mu.Lock()
	_ = s.reloadLocked()
	rec, hasRecord := s.records[absDir]
	s.mu.Unlock()

	if statErr != nil {
		if os.IsNotExist(statErr) {
			if hasRecord {
				return StatusOrphan, &rec, nil, nil
			}
			return StatusMissingCronrc, nil, nil, nil
		}
		return "", nil, nil, fmt.Errorf("checking .cronrc: %w", statErr)
	}

	if fileInfo.IsDir() {
		return StatusMissingCronrc, nil, nil, nil
	}

	data, err := os.ReadFile(cronrcPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("reading .cronrc: %w", err)
	}

	currentHash := cronrc.ComputeHash(data)

	if !hasRecord {
		return StatusUnauthorized, nil, nil, nil
	}

	if rec.Hash != currentHash {
		return StatusChanged, &rec, nil, nil
	}

	return StatusAllowed, &rec, data, nil
}

// Prune removes records for directories that no longer exist or no longer have a .cronrc file.
func (s *Store) Prune() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var pruned []string
	err := s.withFileLock(func() error {
		if err := s.reloadLocked(); err != nil {
			return err
		}
		next := s.cloneRecordsLocked()
		for dir := range next {
			cronrcPath := filepath.Join(dir, ".cronrc")
			if info, err := os.Stat(cronrcPath); err != nil || info.IsDir() {
				delete(next, dir)
				pruned = append(pruned, dir)
			}
		}
		if len(pruned) > 0 {
			return s.saveRecordsLocked(next)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(pruned)
	return pruned, nil
}
