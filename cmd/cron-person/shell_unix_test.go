//go:build unix

package main

import "testing"

func testShell(*testing.T) string { return "/bin/sh" }
