//go:build windows

package history

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const (
	replaceAttempts = 50
	replaceBackoff  = 10 * time.Millisecond
)

// replaceFile atomically replaces dst with src. Windows refuses to replace a
// file another process has open without delete sharing, which Go's readers do
// not request, so a concurrent read fails the rename for the moment it lasts.
func replaceFile(src, dst string) error {
	var err error
	for range replaceAttempts {
		if err = os.Rename(src, dst); !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return err
		}
		time.Sleep(replaceBackoff)
	}
	return err
}
