package cronrc

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/robfig/cron/v3"
)

// Task represents a single scheduled job inside a .cronrc file.
type Task struct {
	LineNumber int    `json:"line_number"`
	Schedule   string `json:"schedule"`
	Command    string `json:"command"`
	Raw        string `json:"raw"`
}

// File represents a parsed .cronrc configuration.
type File struct {
	Path  string            `json:"path"`
	Dir   string            `json:"dir"`
	Hash  string            `json:"hash"`
	Env   map[string]string `json:"env"`
	Tasks []Task            `json:"tasks"`
}

var (
	envVarRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	cronParser  = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
)

// ComputeHash returns the SHA-256 hexadecimal hash of content.
func ComputeHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Parse parses the content of a .cronrc file from an io.Reader.
func Parse(r io.Reader, filePath string) (*File, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}
	dir := filepath.Dir(absPath)

	file := &File{
		Path:  absPath,
		Dir:   dir,
		Env:   make(map[string]string),
		Tasks: make([]Task, 0),
	}

	scanner := bufio.NewScanner(r)
	lineNum := 0
	var rawLines []string

	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()
		rawLines = append(rawLines, raw)
		line := strings.TrimSpace(raw)

		// Ignore empty lines and comment lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check for environment variable assignments (e.g. KEY=VAL or KEY="VAL")
		if envVarRegex.MatchString(line) {
			eqIdx := strings.IndexByte(line, '=')
			key := strings.TrimSpace(line[:eqIdx])
			val := strings.TrimSpace(line[eqIdx+1:])
			// Strip surrounding quotes if present
			if (strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`)) ||
				(strings.HasPrefix(val, `'`) && strings.HasSuffix(val, `'`)) {
				if len(val) >= 2 {
					val = val[1 : len(val)-1]
				}
			}
			file.Env[key] = val
			continue
		}

		// Parse cron schedule and command
		task, parseErr := parseCronLine(line, lineNum, raw)
		if parseErr != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, parseErr)
		}
		file.Tasks = append(file.Tasks, *task)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning cronrc: %w", err)
	}

	file.Hash = ComputeHash([]byte(strings.Join(rawLines, "\n")))
	return file, nil
}

func parseCronLine(line string, lineNum int, raw string) (*Task, error) {
	// Check for descriptor like @daily, @hourly, @weekly, @monthly, @yearly, @annually, @midnight, @reboot
	if strings.HasPrefix(line, "@") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid descriptor line: expected schedule and command")
		}
		schedule := fields[0]
		// Handle @every <duration> which has two tokens for schedule
		var cmdStartIdx int
		if schedule == "@every" {
			if len(fields) < 3 {
				return nil, fmt.Errorf("invalid @every schedule: missing duration and command")
			}
			schedule = fields[0] + " " + fields[1]
			cmdStartIdx = 2
		} else {
			cmdStartIdx = 1
		}

		if schedule != "@reboot" {
			if _, err := cronParser.Parse(schedule); err != nil {
				return nil, fmt.Errorf("invalid schedule descriptor %q: %w", schedule, err)
			}
		}

		command := strings.TrimSpace(strings.Join(fields[cmdStartIdx:], " "))
		return &Task{
			LineNumber: lineNum,
			Schedule:   schedule,
			Command:    command,
			Raw:        raw,
		}, nil
	}

	// Standard 5-field cron: MINUTE HOUR DOM MONTH DOW COMMAND
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return nil, fmt.Errorf("invalid crontab line format: expected at least 5 schedule fields and a command")
	}

	schedule := strings.Join(fields[:5], " ")
	if _, err := cronParser.Parse(schedule); err != nil {
		return nil, fmt.Errorf("invalid cron schedule %q: %w", schedule, err)
	}

	command := strings.TrimSpace(strings.Join(fields[5:], " "))
	return &Task{
		LineNumber: lineNum,
		Schedule:   schedule,
		Command:    command,
		Raw:        raw,
	}, nil
}

// ParseFile reads and parses a .cronrc file from disk.
func ParseFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading cronrc file %s: %w", path, err)
	}

	file, err := Parse(strings.NewReader(string(data)), path)
	if err != nil {
		return nil, err
	}
	file.Hash = ComputeHash(data)
	return file, nil
}

// FindCronrc walks up from the starting directory to find the nearest .cronrc file.
// Returns empty string and nil if no .cronrc is found before the root.
func FindCronrc(startDir string) (string, error) {
	curr, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	for {
		candidate := filepath.Join(curr, ".cronrc")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			// Reached filesystem root
			break
		}
		curr = parent
	}

	return "", nil
}
