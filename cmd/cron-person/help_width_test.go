package main

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// helpScreens are help screens with long descriptions, tree rows, flags, and
// environment variables between them.
var helpScreens = [][]string{{"--help"}, {"exec", "--help"}, {"task", "--help"}, {"crontab", "sync", "--help"}, {"dir", "status", "--help"}}

func renderHelp(t *testing.T, agent, columns string, args []string) string {
	t.Helper()
	t.Setenv("AGENT", agent)
	t.Setenv("COLUMNS", columns)
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("%v: exit %d, stderr %q", args, code, stderr.String())
	}
	return stdout.String()
}

func TestHumanHelpWrapsToColumnsWithoutLosingText(t *testing.T) {
	const width = 40
	for _, args := range helpScreens {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out := renderHelp(t, "0", "40", args)
			for _, line := range strings.Split(out, "\n") {
				// A word longer than the screen stays whole on its own line.
				if utf8.RuneCountInString(line) > width && len(strings.Fields(strings.Trim(line, " │├╰─"))) > 1 {
					t.Errorf("line exceeds %d columns: %q", width, line)
				}
			}
			unwrapped := strings.Join(strings.Fields(renderHelp(t, "0", "", args)), " ")
			for _, word := range strings.Fields(unwrapped) {
				if !strings.Contains(out, word) {
					t.Errorf("wrapped help lost %q", word)
				}
			}
		})
	}
}

func TestUnsizedAndAgentHelpKeepLinesWhole(t *testing.T) {
	for _, tt := range []struct{ name, agent, columns, want string }{
		{"redirected without COLUMNS", "0", "", newExecCommand().Short},
		{"agent mode ignores COLUMNS", "1", "40", "description: " + techCatalog["cron-person exec"].Description},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := renderHelp(t, tt.agent, tt.columns, []string{"exec", "--help"})
			if !strings.Contains(out, tt.want) {
				t.Fatalf("help split %q:\n%s", tt.want, out)
			}
		})
	}
}
