//go:build unix

package cronrc

import (
	"os"
	"syscall"
)

// openNonBlocking opens path without waiting for a FIFO's writer; the flag
// changes nothing for regular files.
func openNonBlocking(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
