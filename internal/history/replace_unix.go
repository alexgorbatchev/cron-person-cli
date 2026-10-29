//go:build unix

package history

import "os"

// replaceFile atomically replaces dst with src.
func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
