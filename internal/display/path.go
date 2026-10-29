// Package display formats paths for diagnostic and descriptive output.
package display

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Path folds the user's home directory for display without changing filesystem paths.
func Path(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	home = filepath.Clean(home)
	if path == home {
		return "~"
	}
	prefix := strings.TrimSuffix(home, string(filepath.Separator)) + string(filepath.Separator)
	if strings.HasPrefix(path, prefix) {
		return "~" + string(filepath.Separator) + strings.TrimPrefix(path, prefix)
	}
	return path
}

// Error formats error messages, including paths carried by wrapped OS errors.
// It is only for diagnostics, never executable output or subprocess streams.
func Error(err error) string {
	message := err.Error()
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		return message
	}
	home = filepath.Clean(home)
	var out strings.Builder
	for {
		i := strings.Index(message, home)
		if i < 0 {
			out.WriteString(message)
			return out.String()
		}
		end := i + len(home)
		left := i == 0 || pathDelimiter(rune(message[i-1]))
		right := end == len(message) || message[end] == byte(filepath.Separator) || pathDelimiter(rune(message[end]))
		out.WriteString(message[:i])
		if left && right {
			out.WriteString("~")
		} else {
			out.WriteString(home)
		}
		message = message[end:]
	}
}

func pathDelimiter(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("\"'`:=()[]{};,", r)
}
