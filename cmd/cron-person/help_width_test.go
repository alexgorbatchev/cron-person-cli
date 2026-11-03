package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

// helpScreens are help screens with long descriptions, tree rows, flags, and
// environment variables between them.
var helpScreens = [][]string{{"--help"}, {"exec", "--help"}, {"task", "--help"}, {"crontab", "sync", "--help"}, {"dir", "status", "--help"}}

// renderHelp renders help with the process stdout redirected to a file, so
// the renderer, which sizes output from the real stdout, never sees the
// terminal running the tests.
func renderHelp(t *testing.T, agent, columns string, args []string) string {
	t.Helper()
	t.Setenv("AGENT", agent)
	t.Setenv("COLUMNS", columns)
	redirected, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = redirected
	defer func() {
		os.Stdout = original
		_ = redirected.Close()
	}()
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("%v: exit %d, stderr %q", args, code, stderr.String())
	}
	return stdout.String()
}

// words returns the text's words in order, dropping the tree rails that
// wrapping repeats on continuation lines.
func words(text string) []string {
	return slices.DeleteFunc(strings.Fields(text), func(w string) bool {
		return strings.Trim(w, "│├╰─") == ""
	})
}

func TestHumanHelpWrapsToColumnsWithoutLosingText(t *testing.T) {
	const width = 40
	for _, args := range helpScreens {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			wrapped := renderHelp(t, "0", "40", args)
			for _, line := range strings.Split(wrapped, "\n") {
				// A word longer than the screen stays whole on its own line.
				if runewidth.StringWidth(line) > width && len(words(line)) > 1 {
					t.Errorf("line exceeds %d columns: %q", width, line)
				}
			}
			if got, want := words(wrapped), words(renderHelp(t, "0", "", args)); !slices.Equal(got, want) {
				t.Errorf("wrapping changed the text:\nwrapped:   %q\nunwrapped: %q", got, want)
			}
		})
	}
}

func TestUnsizedAndAgentHelpStayUnwrapped(t *testing.T) {
	for _, args := range helpScreens {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if unsized, wide := renderHelp(t, "0", "", args), renderHelp(t, "0", "1000", args); unsized != wide {
				t.Errorf("help without COLUMNS or a terminal was wrapped:\n%s\nwant:\n%s", unsized, wide)
			}
			if narrow, unsized := renderHelp(t, "1", "40", args), renderHelp(t, "1", "", args); narrow != unsized {
				t.Errorf("agent help changed with COLUMNS:\n%s\nwant:\n%s", narrow, unsized)
			}
		})
	}
}
