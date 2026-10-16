package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var payloadAssignment = regexp.MustCompile(`(?m)^TOOL_EXECUTABLE="([^"$` + "`" + `\\\r\n]+)"\r?$`)

func bootstrap(source, dotfiles string) (string, error) {
	shim, err := os.ReadFile(filepath.Join(dotfiles, ".generated", "bin", "cron-person"))
	if err != nil {
		return "", fmt.Errorf("reading dotfiles shim: %w", err)
	}
	match := payloadAssignment.FindStringSubmatch(string(shim))
	if len(match) != 2 || strings.Count(string(shim), "TOOL_EXECUTABLE=") != 1 || !filepath.IsAbs(match[1]) {
		return "", fmt.Errorf("shim must contain one literal absolute TOOL_EXECUTABLE path")
	}
	destination, err := filepath.EvalSymlinks(match[1])
	if err != nil {
		return "", fmt.Errorf("resolving payload: %w", err)
	}
	binaries, err := filepath.EvalSymlinks(filepath.Join(dotfiles, ".generated", "binaries"))
	if err != nil {
		return "", fmt.Errorf("resolving binaries directory: %w", err)
	}
	if !strings.HasPrefix(destination, binaries+string(filepath.Separator)) {
		return "", fmt.Errorf("payload must be inside dotfiles binaries directory")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("reading build: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("build output is empty")
	}
	tmp := filepath.Join(dotfiles, ".tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	tmp, err = os.MkdirTemp(tmp, "cron-person-bootstrap-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) // Best-effort cleanup after a failed install.
	staged := filepath.Join(tmp, "cron-person")
	if err := os.WriteFile(staged, data, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(staged, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func run() error {
	dotfiles := os.Getenv("dotfiles_dir")
	if dotfiles == "" {
		return fmt.Errorf("run through just dev-bootstrap")
	}
	destination, err := bootstrap("bin/cron-person", dotfiles)
	if err != nil {
		return err
	}
	fmt.Println("Installed local cron-person build at " + destination)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
