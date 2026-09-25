package crontab

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

const (
	BlockStart = "# >>> cron-person managed block (DO NOT EDIT DIRECTLY) >>>"
	BlockEnd   = "# <<< cron-person managed block <<<"
)

var blockRegex = regexp.MustCompile(`(?s)# >>> cron-person managed block \(DO NOT EDIT DIRECTLY\) >>>.*?# <<< cron-person managed block <<<(\n)?`)

// GenerateBlock builds the managed crontab string from all authorized records.
func GenerateBlock(records []store.Record, binPath string) (string, error) {
	if binPath == "" {
		executable, err := os.Executable()
		if err != nil {
			binPath = "cron-person"
		} else {
			binPath = executable
		}
	}

	var sb strings.Builder
	sb.WriteString(BlockStart)
	sb.WriteString("\n")

	for _, rec := range records {
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
			sb.WriteString(fmt.Sprintf("%s %s exec %s -- %s\n", task.Schedule, binPath, rec.Dir, task.Command))
		}
	}

	sb.WriteString(BlockEnd)
	return sb.String(), nil
}

// MergeCrontab merges the managed block into existing crontab text.
func MergeCrontab(existing string, managedBlock string) string {
	cleaned := RemoveBlock(existing)
	cleaned = strings.TrimRight(cleaned, "\n")

	if managedBlock == "" {
		if cleaned == "" {
			return ""
		}
		return cleaned + "\n"
	}

	if cleaned == "" {
		return managedBlock + "\n"
	}

	return cleaned + "\n\n" + managedBlock + "\n"
}

// RemoveBlock strips the managed block from crontab text.
func RemoveBlock(existing string) string {
	return blockRegex.ReplaceAllString(existing, "")
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

	managedBlock, err := GenerateBlock(validRecords, binPath)
	if err != nil {
		return 0, fmt.Errorf("generating managed crontab block: %w", err)
	}

	existing, err := ReadSystemCrontab()
	if err != nil {
		return 0, err
	}

	updated := MergeCrontab(existing, managedBlock)
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

	cleaned := RemoveBlock(existing)
	return WriteSystemCrontab(cleaned)
}
