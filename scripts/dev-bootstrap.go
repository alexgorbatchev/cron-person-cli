package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func commandOutput(directory, executable string, args ...string) (string, error) {
	command := exec.Command(executable, args...)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s failed: %s: %w", executable, strings.TrimSpace(string(failure.Stderr)), err)
		}
		return "", fmt.Errorf("running %s: %w", executable, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func bootstrap(source, dotfiles string) (string, error) {
	directory, err := filepath.Abs(dotfiles)
	if err != nil {
		return "", fmt.Errorf("resolving dotfiles directory: %w", err)
	}
	help, err := commandOutput(directory, "dotfiles", "tool", "dev", "use", "--help")
	if err != nil || !strings.Contains(help, "--bin") {
		return "", fmt.Errorf("update dotfiles to a version supporting 'tool dev use' before bootstrapping")
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("resolving build path: %w", err)
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("reading build: %w", err)
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("build output is empty")
	}
	repository, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving source repository: %w", err)
	}
	version, err := commandOutput(repository, source, "--version")
	if err != nil {
		return "", err
	}
	_, err = commandOutput(directory, "dotfiles", "tool", "dev", "use", "cron-person",
		"--bin", "cron-person="+source, "--source-repo", repository, "--version", version)
	if err != nil {
		return "", fmt.Errorf("registering development build: %w", err)
	}
	return commandOutput(directory, "dotfiles", "tool", "which", "cron-person", "--bin")
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
	fmt.Println("Registered local cron-person build at " + destination)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
