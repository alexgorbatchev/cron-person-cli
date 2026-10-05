package main

import (
	"strings"
	"testing"
)

func TestCLIExecAndTaskRun(t *testing.T) {
	for _, prefix := range [][]string{{"exec"}, {"task", "run"}} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			t.Setenv("SHELL", "/bin/sh")
			dir, state := cliProject(t)
			requireCLIOutput(t, []string{"--store", state, "allow", dir})
			args := append([]string{"--store", state}, prefix...)
			args = append(args, dir, "--", `printf '%s\n' "$CLI_VALUE" "$PWD"`)
			requireCLIOutput(t, args, "from-file\n"+dir+"\n")
			args[len(args)-1] = "printf failed >&2; exit 3"
			out, err := executeCommand(args...)
			if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(out, "failed") {
				t.Fatalf("command failure error=%v output=%q", err, out)
			}
		})
	}
}

func TestRootDefaultHelp(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run("agent="+mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			requireCLIOutput(t, nil, "cron-person", "crontab", "task")
		})
	}
}
