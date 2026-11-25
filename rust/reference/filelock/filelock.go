// Package filelockreference characterizes the real OS-backed filelock package.
package filelockreference

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/alexgorbatchev/cron-person-cli/internal/filelock"
)

func outcome(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, filelock.ErrLocked) {
		return filelock.ErrLocked.Error()
	}
	var errno syscall.Errno
	if errors.Is(err, os.ErrClosed) {
		return "file already closed"
	}
	if errors.As(err, &errno) {
		return fmt.Sprintf("os:%d", errno)
	}
	return err.Error()
}

// Scenario runs native operations in an owned fixture and returns data observations.
// OS errors are represented by their numeric codes; paths never enter the result.
func Scenario(name string) (string, error) {
	text := strings.HasPrefix(name, "text-")
	name = strings.TrimPrefix(name, "text-")
	valid := map[string]bool{}
	for _, n := range []string{"contention", "close", "release", "double-release", "missing-parent", "directory", "nul", "closed", "read-only", "retained", "clone", "non-utf8"} {
		valid[n] = true
	}
	if !valid[name] {
		return "", errors.New("unknown filelock scenario")
	}
	dir, err := os.MkdirTemp("", "filelock-go-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	return runScenario(name, dir, text)
}

func runScenario(name, dir string, text bool) (string, error) {
	path := filepath.Join(dir, "lock")
	if text {
		var observed error
		switch name {
		case "missing-parent":
			_, observed = filelock.LockPath(filepath.Join(dir, "missing", "lock"))
		case "directory":
			_, observed = filelock.LockPath(dir)
		case "nul":
			_, observed = filelock.LockPath(path + "\x00")
		case "non-utf8":
			if runtime.GOOS == "windows" {
				return "", errors.New("requires Unix path bytes")
			}
			_, observed = filelock.LockPath(filepath.Join(dir, "missing", string([]byte{0xff})))
		case "double-release":
			release, err := filelock.LockPath(path)
			if err != nil {
				return "", err
			}
			if err := release(); err != nil {
				return "", err
			}
			observed = release()
		case "closed":
			file, err := os.Create(path)
			if err != nil {
				return "", err
			}
			if err := file.Close(); err != nil {
				return "", err
			}
			observed = filelock.Lock(file)
		default:
			return "", errors.New("unknown error-text scenario")
		}
		if observed == nil {
			return "", errors.New("expected native error")
		}
		message := strings.ReplaceAll(observed.Error(), dir, "<fixture>")
		if name == "non-utf8" {
			return hex.EncodeToString([]byte(message)), nil
		}
		return message, nil
	}
	var events []string
	add := func(err error) { events = append(events, outcome(err)) }
	if name == "missing-parent" || name == "directory" || name == "nul" {
		if name == "missing-parent" {
			path = filepath.Join(dir, "missing", "lock")
		}
		if name == "directory" {
			path = dir
		}
		if name == "nul" {
			path += "\x00"
		}
		release, err := filelock.LockPath(path)
		add(err)
		if err == nil {
			add(release())
		}
	} else if name == "release" || name == "double-release" || name == "retained" {
		if name == "retained" {
			if err := os.WriteFile(path, []byte("retained"), 0o600); err != nil {
				return "", err
			}
		}
		release, err := filelock.LockPath(path)
		if err != nil {
			return "", err
		}
		add(err)
		probe, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			_ = release()
			return "", err
		}
		defer probe.Close()
		add(filelock.TryLock(probe))
		add(release())
		add(filelock.TryLock(probe))
		add(filelock.Unlock(probe))
		if name == "double-release" {
			// Record every leaf of errors.Join, preserving unlock and close failure.
			joined := release().(interface{ Unwrap() []error })
			for _, e := range joined.Unwrap() {
				add(e)
			}
		}
		if name == "retained" {
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			events = append(events, string(data))
		}
	} else {
		holder, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return "", err
		}
		defer holder.Close()
		probe, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return "", err
		}
		defer probe.Close()
		switch name {
		case "closed":
			add(holder.Close())
			add(filelock.Lock(holder))
			add(filelock.TryLock(holder))
			add(filelock.Unlock(holder))
		case "read-only":
			reader, err := os.Open(path)
			if err != nil {
				return "", err
			}
			defer reader.Close()
			add(filelock.TryLock(reader))
			add(filelock.Unlock(reader))
		case "clone":
			// Cross-process inherited-descriptor semantics are tested by the native harness.
			add(filelock.Lock(holder))
			add(filelock.Unlock(holder))
			add(filelock.TryLock(probe))
		default:
			add(filelock.Lock(holder))
			add(filelock.TryLock(probe))
			if name == "close" {
				add(holder.Close())
			} else {
				add(filelock.Unlock(holder))
			}
			add(filelock.TryLock(probe))
			add(filelock.TryLock(holder))
			add(filelock.Unlock(probe))
		}
	}
	data, err := json.Marshal(events)
	return string(data), err
}
