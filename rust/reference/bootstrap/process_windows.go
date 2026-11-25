package bootstrapreference

import "os/exec"

// Shell fixtures are explicitly rejected on Windows before subprocess creation.
func configure(cmd *exec.Cmd) {}
