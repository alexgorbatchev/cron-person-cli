package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v3"
	"github.com/spf13/cobra"
)

const skillAlert = "ALERT: Agents must read `AGENT=1 cron-person skill` before using this tool.\n"

const skillInstructions = `Install the cron-person skill using the Skills CLI:

  npx skills add alexgorbatchev/cron-person-cli --skill cron-person

Skill:

  https://github.com/alexgorbatchev/cron-person-cli/tree/main/skills/cron-person
`

var skillPath = filepath.Join("..", "..", "skills", "cron-person", "SKILL.md")

func readSkill(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSkillCommandOutput(t *testing.T) {
	for _, agent := range []string{"0", "1"} {
		t.Setenv("AGENT", agent)
		var stdout, stderr bytes.Buffer
		if code := run([]string{"skill"}, &stdout, &stderr); code != 0 || stdout.String() != skillInstructions || stderr.Len() != 0 {
			t.Fatalf("AGENT=%s: exit %d stdout %q stderr %q", agent, code, stdout.String(), stderr.String())
		}
		if code := run([]string{"skill", "extra"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "unknown command") {
			t.Fatalf("AGENT=%s: extra argument accepted: exit %d stderr %q", agent, code, stderr.String())
		}
	}
}

func TestSkillCommandReportsWriteFailure(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"skill"}, failingWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "writing the skill: stdout write failed") {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
}

func TestSkillPointerMatchesRepositorySkill(t *testing.T) {
	const prefix = "https://github.com/alexgorbatchev/cron-person-cli/tree/main/"
	i := strings.Index(skillInstructions, prefix)
	if i < 0 {
		t.Fatalf("skill URL missing from %q", skillInstructions)
	}
	dir := strings.TrimSpace(skillInstructions[i+len(prefix):])
	if filepath.Join("..", "..", dir, "SKILL.md") != skillPath {
		t.Fatalf("printed skill path %q does not point at %s", dir, skillPath)
	}
	if !strings.Contains(readSkill(t), "\nname: cron-person\n") || !strings.Contains(skillInstructions, "--skill cron-person\n") {
		t.Fatal("SKILL.md frontmatter name does not match the printed --skill value")
	}
}

func TestSkillDocumentsEveryCommandAndFlag(t *testing.T) {
	if missing := cobrahelptree.SkillOmissions(newRootCommand(), techCatalog, readSkill(t)); len(missing) > 0 {
		t.Fatalf("SKILL.md omits:\n%s", strings.Join(missing, "\n"))
	}
}

func TestAgentHelpOpensWithSkillAlert(t *testing.T) {
	var paths [][]string
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		paths = append(paths, path)
		for _, sub := range c.Commands() {
			if sub.IsAvailableCommand() && sub.Name() != "skill" {
				walk(sub, append(append([]string{}, path...), sub.Name()))
			}
		}
	}
	walk(newRootCommand(), nil)
	if len(paths) < 20 {
		t.Fatalf("walked only %d commands", len(paths))
	}
	for _, path := range paths {
		for _, agent := range []string{"1", "0"} {
			t.Setenv("AGENT", agent)
			var stdout, stderr bytes.Buffer
			run(append(append([]string{}, path...), "--help"), &stdout, &stderr)
			if hasAlert := strings.HasPrefix(stdout.String(), skillAlert); hasAlert != (agent == "1") {
				t.Fatalf("AGENT=%s %v --help alert=%v:\n%.200s", agent, path, hasAlert, stdout.String())
			}
		}
	}
}
