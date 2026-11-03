package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

// capacityDirs are the tracked-directory counts measured: a typical
// workstation (10), a heavy one (100), and a stress case (1000). Each
// .cronrc holds two variables and ten tasks, about 400 bytes.
var capacityDirs = []int{10, 100, 1000}

// trackedStore builds a store authorizing n directories and returns its path
// and one of the directories.
func trackedStore(b *testing.B, n int) (state, project string) {
	b.Helper()
	// Measure the human output whatever the caller's environment says.
	b.Setenv("AGENT", "0")
	root := b.TempDir()
	state = filepath.Join(root, "state", "allowed.json")
	st, err := store.NewStore(state)
	if err != nil {
		b.Fatal(err)
	}
	var cronrc strings.Builder
	cronrc.WriteString("APP_ENV=production\nPATH=/usr/local/bin:/usr/bin:/bin\n")
	for i := range 10 {
		fmt.Fprintf(&cronrc, "%d */2 * * * ./scripts/task-%d.sh --verbose\n", i*5, i)
	}
	for i := range n {
		project = filepath.Join(root, fmt.Sprintf("project-%04d", i))
		if err := os.Mkdir(project, 0o700); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, ".cronrc"), []byte(cronrc.String()), 0o600); err != nil {
			b.Fatal(err)
		}
		if _, err := st.Allow(project); err != nil {
			b.Fatal(err)
		}
	}
	return state, project
}

func benchmarkCommand(b *testing.B, args ...string) {
	for _, n := range capacityDirs {
		b.Run(fmt.Sprintf("dirs=%d", n), func(b *testing.B) {
			state, _ := trackedStore(b, n)
			full := append([]string{"--store", state, "--config", filepath.Join(b.TempDir(), "config.json")}, args...)
			for b.Loop() {
				if out, err := executeCommand(full...); err != nil {
					b.Fatalf("%v: %v\n%s", args, err, out)
				}
			}
		})
	}
}

// BenchmarkStatus measures the overview, which inspects every directory.
func BenchmarkStatus(b *testing.B) { benchmarkCommand(b, "status") }

// BenchmarkTaskList measures listing and describing every task.
func BenchmarkTaskList(b *testing.B) { benchmarkCommand(b, "task", "list") }

// BenchmarkCrontabShow measures generating the managed block.
func BenchmarkCrontabShow(b *testing.B) { benchmarkCommand(b, "crontab", "show") }

// BenchmarkHookExport measures the check the shell hook runs on every
// prompt, inside one tracked directory.
func BenchmarkHookExport(b *testing.B) {
	for _, n := range capacityDirs {
		b.Run(fmt.Sprintf("dirs=%d", n), func(b *testing.B) {
			state, project := trackedStore(b, n)
			b.Chdir(project)
			for b.Loop() {
				if out, err := executeCommand("--store", state, "hook", "export", "bash"); err != nil || out != "" {
					b.Fatalf("hook export: %v %q", err, out)
				}
			}
		})
	}
}

// BenchmarkAllow measures one authorization change, which rewrites and
// flushes the whole store.
func BenchmarkAllow(b *testing.B) {
	for _, n := range capacityDirs {
		b.Run(fmt.Sprintf("dirs=%d", n), func(b *testing.B) {
			state, project := trackedStore(b, n)
			for b.Loop() {
				if out, err := executeCommand("--store", state, "allow", project); err != nil {
					b.Fatalf("allow: %v\n%s", err, out)
				}
			}
		})
	}
}
