package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
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

func checkCoverageFile(profilePath string, packages []string, minPercent float64) (string, error) {
	if len(packages) == 0 {
		return "", fmt.Errorf("coverage_packages must list at least one Go package")
	}
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return "", fmt.Errorf("reading coverage profile: %w", err)
	}

	type counts struct {
		covered int
		total   int
	}
	stats := make(map[string]counts, len(packages))

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		loc, _, hasColon := strings.Cut(fields[0], ":")
		if len(fields) != 3 || !hasColon {
			return "", fmt.Errorf("malformed coverage profile line: %q", line)
		}
		stmts, stmtErr := strconv.Atoi(fields[1])
		hits, hitErr := strconv.Atoi(fields[2])
		if stmtErr != nil || hitErr != nil || stmts < 0 || hits < 0 {
			return "", fmt.Errorf("invalid coverage counts in line: %q", line)
		}
		pkg := path.Dir(loc)
		cur := stats[pkg]
		cur.total += stmts
		if hits > 0 {
			cur.covered += stmts
		}
		stats[pkg] = cur
	}

	var report strings.Builder
	var underCovered []string
	for _, pkg := range packages {
		cur := stats[pkg]
		pct := 0.0
		if cur.total > 0 {
			pct = (float64(cur.covered) * 100.0) / float64(cur.total)
		}
		fmt.Fprintf(&report, "%s: %d/%d statements (%.1f%%)\n", pkg, cur.covered, cur.total, pct)
		if cur.total == 0 || pct < minPercent {
			underCovered = append(underCovered, fmt.Sprintf("%s (%.1f%% < %.1f%%)", pkg, pct, minPercent))
		}
	}

	if len(underCovered) > 0 {
		return report.String(), fmt.Errorf("packages below %.1f%% statement coverage floor: %s", minPercent, strings.Join(underCovered, ", "))
	}
	return report.String(), nil
}

func run() error {
	if profile := os.Getenv("coverage_profile"); profile != "" {
		report, err := checkCoverageFile(profile, strings.Fields(os.Getenv("coverage_packages")), 90.0)
		fmt.Print(report)
		return err
	}
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
