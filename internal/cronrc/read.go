package cronrc

import (
	"errors"
	"fmt"
	"io"
)

// MaxSize bounds how much of a .cronrc is read, so an oversized file cannot
// stall the prompt hook or exhaust memory. Real crontabs are a few kilobytes.
const MaxSize = 1 << 20

var (
	// ErrNotRegular reports a .cronrc that is a directory, FIFO, device, or
	// socket rather than a regular file.
	ErrNotRegular = errors.New("not a regular file")
	// ErrTooLarge reports a .cronrc larger than MaxSize.
	ErrTooLarge = fmt.Errorf("larger than %d bytes", MaxSize)
)

// ReadFile reads a .cronrc, following symlinks. It opens the file without
// blocking and checks the opened file's type, so a FIFO or device named
// .cronrc, even one swapped in after a check, is rejected instead of waiting
// for a writer.
func ReadFile(path string) ([]byte, error) {
	f, err := openNonBlocking(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() // Read-only; nothing to flush.
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSize {
		return nil, ErrTooLarge
	}
	return data, nil
}
