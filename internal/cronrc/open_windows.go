package cronrc

import "os"

// openNonBlocking opens path normally: Windows has no FIFOs in the file
// system namespace that an open could wait on.
func openNonBlocking(path string) (*os.File, error) {
	return os.Open(path)
}
