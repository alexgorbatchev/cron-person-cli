package store

// syncDir is a no-op: Windows cannot flush a directory through os.File, so
// whether a rename survives power loss there is left to the file system.
func syncDir(string) error { return nil }
