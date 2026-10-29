package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskListSourceAndDescription(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run("agent="+mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			home := t.TempDir()
			t.Setenv("HOME", home)
			state := filepath.Join(home, "allowed.json")
			for _, name := range []string{"first", "second"} {
				dir := filepath.Join(home, name)
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				writeCLIFile(t, filepath.Join(dir, ".cronrc"), "# project schedule\n\nSHELL=/bin/sh\n*/30 * * * * echo "+name+"\n@reboot echo startup\n")
				requireCLIOutput(t, []string{"--store", state, "allow", dir}, "")
			}
			out := requireCLIOutput(t, []string{"--store", state, "task", "list"},
				"Every 30 minutes", "*/30 * * * *", "At system startup", "@reboot", "echo first", "echo second")
			for _, name := range []string{"first", "second"} {
				if mode == "1" {
					for _, line := range []string{"4", "5"} {
						want := "dir: ~/" + name + "\tfile: ~/" + name + "/.cronrc\tline: " + line
						if !strings.Contains(out, want) {
							t.Errorf("missing source %q in %s", want, out)
						}
					}
				} else {
					for _, want := range []string{"Directory: ~/" + name, "Source: ~/" + name + "/.cronrc:4", "Source: ~/" + name + "/.cronrc:5", "Command: echo " + name} {
						if !strings.Contains(out, want) {
							t.Errorf("missing %q in %s", want, out)
						}
					}
				}
			}
			if strings.Contains(out, home) {
				t.Errorf("unfolded home directory in output: %s", out)
			}
		})
	}
}
