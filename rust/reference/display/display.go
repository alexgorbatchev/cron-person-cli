// Package displayreference exposes lossless data boundaries for migration checks.
package displayreference

import (
	"encoding/base64"
	"errors"
	"os"
	"runtime"
	"sync"

	"github.com/alexgorbatchev/cron-person-cli/internal/display"
)

var homeMu sync.Mutex

// FormatPath calls the production formatter with the process environment.
func FormatPath(value string) string { return display.Path(value) }

// FormatErrorText calls the production error formatter with a real error.
func FormatErrorText(value string) string { return display.Error(errors.New(value)) }

// PathBytes preserves arbitrary Go string bytes and selects an isolated HOME input.
func PathBytes(value, home string, present bool) (string, error) {
	return formatBytes(value, home, present, display.Path)
}

// ErrorBytes preserves arbitrary diagnostic bytes through the JSON bridge.
func ErrorBytes(value, home string, present bool) (string, error) {
	return formatBytes(value, home, present, func(value string) string { return display.Error(errors.New(value)) })
}

func formatBytes(value, home string, present bool, format func(string) string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", errors.New("invalid value base64")
	}
	homeData, err := base64.StdEncoding.DecodeString(home)
	if err != nil {
		return "", errors.New("invalid home base64")
	}
	homeMu.Lock()
	defer homeMu.Unlock()
	key := "HOME"
	if runtime.GOOS == "windows" {
		key = "USERPROFILE"
	}
	old, existed := os.LookupEnv(key)
	defer func() {
		if existed {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	}()
	if present {
		err = os.Setenv(key, string(homeData))
	} else {
		err = os.Unsetenv(key)
	}
	if err != nil {
		return "", errors.New("invalid HOME environment")
	}
	return base64.StdEncoding.EncodeToString([]byte(format(string(data)))), nil
}
