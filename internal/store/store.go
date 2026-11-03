package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/filelock"
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
	if stateHome := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(stateHome) {
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
	if err := createDirDurably(filepath.Dir(s.filePath)); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	release, err := filelock.LockPath(s.filePath + ".lock")
	if err != nil {
		return fmt.Errorf("locking store file: %w", err)
	}
	defer release() // Best effort: the change already succeeded or failed.

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

	s.removeStaleTempFilesLocked()
	tmpFile, err := os.CreateTemp(filepath.Dir(s.filePath), filepath.Base(s.filePath)+tempFileSuffix)
	if err != nil {
		return fmt.Errorf("writing store temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	_, writeErr := tmpFile.Write(data)
	syncErr := tmpFile.Sync()
	closeErr := tmpFile.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing store temp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("committing store file: %w", err)
	}
	// The rename is visible from here on, so memory follows disk even when
	// flushing the directory entry fails.
	s.records = records
	if err := syncDir(filepath.Dir(s.filePath)); err != nil {
		return fmt.Errorf("flushing store directory: %w", err)
	}
	return nil
}

// createDirDurably creates dir and its missing parents and flushes each new
// entry into its parent, so a first change's store survives power loss along
// with the directories that hold it.
func createDirDurably(dir string) error {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		_, err := os.Stat(d)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		missing = append(missing, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, d := range missing {
		if err := syncDir(filepath.Dir(d)); err != nil {
			return err
		}
	}
	return nil
}

// tempFileSuffix is the os.CreateTemp pattern for staged store writes; the
// random part is a decimal number.
const tempFileSuffix = ".*.tmp"

// removeStaleTempFilesLocked deletes staged writes left by writers that died
// before renaming them. Writers stage files only while holding the store
// lock, so under the lock every staged file is abandoned. Removal is best
// effort: a leftover file is never read, and failing to delete one must not
// block an authorization change.
func (s *Store) removeStaleTempFilesLocked() {
	dir := filepath.Dir(s.filePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := filepath.Base(s.filePath) + "."
	for _, entry := range entries {
		name := entry.Name()
		random, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		random, ok = strings.CutSuffix(random, ".tmp")
		if !ok || random == "" || strings.Trim(random, "0123456789") != "" {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
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
func (s *Store) Get(dir string) (*Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.reloadLocked(); err != nil {
		return nil, false, err
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, false, fmt.Errorf("resolving directory path: %w", err)
	}

	rec, ok := s.records[absDir]
	if !ok {
		return nil, false, nil
	}
	return &rec, true, nil
}

// List returns a sorted copy of all stored records.
func (s *Store) List() ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.reloadLocked(); err != nil {
		return nil, err
	}

	list := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		list = append(list, rec)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Dir < list[j].Dir
	})
	return list, nil
}

// Status inspects disk and compares with store to determine the directory's status.
func (s *Store) Status(dir string) (DirStatus, *Record, error) {
	status, rec, _, err := s.Inspect(dir)
	return status, rec, err
}

// Inspect reads a single .cronrc snapshot from dir and compares its raw-byte hash against the store.
// It returns the snapshot whenever the file was read, so callers describe exactly the
// content the status was computed from; only StatusAllowed authorizes executing it.
func (s *Store) Inspect(dir string) (DirStatus, *Record, []byte, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolving path: %w", err)
	}

	data, readErr := cronrc.ReadFile(filepath.Join(absDir, ".cronrc"))

	s.mu.Lock()
	if err := s.reloadLocked(); err != nil {
		s.mu.Unlock()
		return "", nil, nil, err
	}
	rec, hasRecord := s.records[absDir]
	s.mu.Unlock()

	switch {
	case errors.Is(readErr, fs.ErrNotExist):
		if hasRecord {
			return StatusOrphan, &rec, nil, nil
		}
		return StatusMissingCronrc, nil, nil, nil
	case errors.Is(readErr, cronrc.ErrNotRegular):
		// Only a regular file can be authorized, so anything else counts as
		// no .cronrc at all.
		return StatusMissingCronrc, nil, nil, nil
	case readErr != nil:
		return "", nil, nil, fmt.Errorf("checking .cronrc: %w", readErr)
	}

	currentHash := cronrc.ComputeHash(data)

	if !hasRecord {
		return StatusUnauthorized, nil, data, nil
	}

	if rec.Hash != currentHash {
		return StatusChanged, &rec, data, nil
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
