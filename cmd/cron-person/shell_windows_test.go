//go:build windows

package main

import (
	"os/exec"
	"testing"
)

// testShell is the POSIX shell Git for Windows provides.
func testShell(t *testing.T) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("needs bash on PATH")
	}
	return bash
}
