package crontab

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

const (
	BlockStart = "# >>> cron-person managed block (DO NOT EDIT DIRECTLY) >>>"
	BlockEnd   = "# <<< cron-person managed block <<<"
)

// GenerateBlock builds the managed crontab string from all authorized records.
func GenerateBlock(records []store.Record, binPath, storePath string) (string, error) {
	if strings.TrimSpace(storePath) == "" {
		return "", fmt.Errorf("authorization store path cannot be empty")
	}
	storePath, err := filepath.Abs(storePath)
	if err != nil {
		return "", fmt.Errorf("resolving authorization store: %w", err)
	}
	if binPath == "" {
		executable, err := os.Executable()
		if err != nil {
			binPath = "cron-person"
		} else {
			binPath = executable
		}
	}
	if strings.ContainsAny(binPath+storePath, "\r\n\x00") {
		return "", fmt.Errorf("executable and store paths must not contain line breaks or NUL")
	}

	var sb strings.Builder
	sb.WriteString(BlockStart)
	sb.WriteString("\n")

	for _, rec := range records {
		if strings.TrimSpace(rec.Dir) == "" || strings.ContainsAny(rec.Dir, "\r\n\x00") {
			return "", fmt.Errorf("directory path must not be empty or contain line breaks or NUL")
		}
		parsed, err := cronrc.ParseFile(rec.Path)
		if err != nil {
			// Skip files that fail parsing or are missing
			continue
		}

		if len(parsed.Tasks) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("# Directory: %s\n", rec.Dir))
		for _, task := range parsed.Tasks {
			if strings.ContainsAny(task.Schedule+task.Command, "\r\n\x00") {
				return "", fmt.Errorf("task in %s must not contain line breaks or NUL", rec.Path)
			}
			sb.WriteString(fmt.Sprintf("%s %s --store %s exec %s -- %s\n", task.Schedule,
				quoteCronArg(binPath), quoteCronArg(storePath), quoteCronArg(rec.Dir), quoteCronArg(task.Command)))
		}
	}

	sb.WriteString(BlockEnd)
	return sb.String(), nil
}

// Keep shell syntax inside a single argument for the authorized runner. Cron
// processes percent signs and consecutive backslashes before the shell, even
// inside quotes; isolate each escaped percent outside quotes and separate
// adjacent backslashes across single-quoted segments.
func quoteCronArg(s string) string {
	var sb strings.Builder
	sb.WriteByte('\'')
	prevBackslash := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\'':
			sb.WriteString(`'"'"'`)
			prevBackslash = false
		case '%':
			sb.WriteString(`'\%'`)
			prevBackslash = false
		case '\\':
			if prevBackslash {
				sb.WriteString(`''`)
			}
			sb.WriteByte('\\')
			prevBackslash = true
		default:
			sb.WriteByte(ch)
			prevBackslash = false
		}
	}
	sb.WriteByte('\'')
	return sb.String()
}

// MergeCrontab merges the managed block into existing crontab text.
func MergeCrontab(existing string, managedBlock string) (string, error) {
	cleaned, err := RemoveBlock(existing)
	if err != nil {
		return "", err
	}
	cleaned = strings.TrimRight(cleaned, "\n")

	if managedBlock == "" {
		if cleaned == "" {
			return "", nil
		}
		return cleaned + "\n", nil
	}

	if cleaned == "" {
		return managedBlock + "\n", nil
	}

	return cleaned + "\n\n" + managedBlock + "\n", nil
}

// RemoveBlock strips the managed block from crontab text.
func RemoveBlock(existing string) (string, error) {
	blockStartOffset := -1
	blockEndOffset := -1

	for offset := 0; offset < len(existing); {
		lineEnd := len(existing)
		if nl := strings.IndexByte(existing[offset:], '\n'); nl >= 0 {
			lineEnd = offset + nl + 1
		}
		line := strings.TrimSuffix(existing[offset:lineEnd], "\n")
		line = strings.TrimSuffix(line, "\r")

		switch line {
		case BlockStart:
			if blockStartOffset >= 0 && blockEndOffset < 0 {
				return "", fmt.Errorf("malformed crontab: nested %q without matching end marker", BlockStart)
			}
			if blockEndOffset >= 0 {
				return "", fmt.Errorf("ambiguous crontab: multiple cron-person managed blocks found")
			}
			blockStartOffset = offset
		case BlockEnd:
			if blockStartOffset < 0 || blockEndOffset >= 0 {
				return "", fmt.Errorf("malformed crontab: unexpected %q without matching start marker", BlockEnd)
			}
			blockEndOffset = lineEnd
		}

		offset = lineEnd
	}

	if blockStartOffset >= 0 && blockEndOffset < 0 {
		return "", fmt.Errorf("malformed crontab: unclosed %q without matching end marker", BlockStart)
	}
	if blockStartOffset < 0 {
		return existing, nil
	}

	return existing[:blockStartOffset] + existing[blockEndOffset:], nil
}

// ReadSystemCrontab executes `crontab -l` to get the current user's crontab.
func ReadSystemCrontab() (string, error) {
	cmd := exec.Command("crontab", "-l")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// "no crontab for <user>" is a normal condition, return empty string
		errStr := stderr.String()
		if strings.Contains(errStr, "no crontab for") {
			return "", nil
		}
		return "", fmt.Errorf("reading system crontab (crontab -l): %w: %s", err, errStr)
	}

	return stdout.String(), nil
}

// WriteSystemCrontab installs crontab content using `crontab -`.
func WriteSystemCrontab(content string) error {
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(content)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installing crontab: %w: %s", err, stderr.String())
	}
	return nil
}

// Sync reads the store, generates the managed block, and updates the system crontab.
func Sync(st *store.Store, binPath string) (int, error) {
	records := st.List()
	validRecords := make([]store.Record, 0, len(records))

	for _, rec := range records {
		status, _, err := st.Status(rec.Dir)
		if err == nil && status == store.StatusAllowed {
			validRecords = append(validRecords, rec)
		}
	}

	managedBlock, err := GenerateBlock(validRecords, binPath, st.Path())
	if err != nil {
		return 0, fmt.Errorf("generating managed crontab block: %w", err)
	}

	existing, err := ReadSystemCrontab()
	if err != nil {
		return 0, err
	}

	updated, err := MergeCrontab(existing, managedBlock)
	if err != nil {
		return 0, err
	}
	if err := WriteSystemCrontab(updated); err != nil {
		return 0, err
	}

	return len(validRecords), nil
}

// Uninstall removes the managed block from the system crontab.
func Uninstall() error {
	existing, err := ReadSystemCrontab()
	if err != nil {
		return err
	}

	cleaned, err := RemoveBlock(existing)
	if err != nil {
		return err
	}
	return WriteSystemCrontab(cleaned)
}
