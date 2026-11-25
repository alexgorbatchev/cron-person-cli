package filelockreference

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/filelock"
)

func TestScenarios(t *testing.T) {
	for _, name := range []string{"contention", "close", "release", "double-release", "missing-parent", "directory", "nul", "closed", "read-only", "retained", "clone"} {
		t.Run(name, func(t *testing.T) {
			value, err := Scenario(name)
			if err != nil || value == "" {
				t.Fatalf("%s: %q %v", name, value, err)
			}
		})
	}
	if _, err := Scenario("unknown"); err == nil {
		t.Fatal("missing scenario accepted")
	}
	for _, name := range []string{"text-missing-parent", "text-directory", "text-nul", "text-closed", "text-double-release"} {
		if value, err := Scenario(name); err != nil || value == "" {
			t.Fatalf("%s: %q %v", name, value, err)
		}
	}
	if _, err := Scenario("text-close"); err == nil {
		t.Fatal("missing text scenario accepted")
	}
	value, err := Scenario("text-non-utf8")
	if runtime.GOOS == "windows" {
		if err == nil || err.Error() != "requires Unix path bytes" {
			t.Fatalf("unexpected Unix-only scenario result: %q %v", value, err)
		}
	} else if err != nil || !strings.Contains(value, "ff") {
		t.Fatalf("missing native non-UTF-8 error bytes: %q %v", value, err)
	}
}

func TestFixtureAndOutcomeErrors(t *testing.T) {
	missing := t.TempDir() + "/missing"
	for _, name := range []string{"contention", "retained", "release"} {
		if _, err := runScenario(name, missing, false); err == nil {
			t.Fatalf("%s: unusable fixture accepted", name)
		}
	}
	for _, name := range []string{"double-release", "closed"} {
		if _, err := runScenario(name, missing, true); err == nil {
			t.Fatalf("%s: unusable fixture accepted", name)
		}
	}
	if got := outcome(errors.New("other")); got != "other" {
		t.Fatal(got)
	}
	t.Setenv("TMPDIR", t.TempDir()+"/missing")
	t.Setenv("TMP", os.Getenv("TMPDIR"))
	t.Setenv("TEMP", os.Getenv("TMPDIR"))
	if _, err := Scenario("contention"); err == nil {
		t.Fatal("missing temporary directory accepted")
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("FILELOCK_PROCESS") == "1" {
		if err := process(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func process() error {
	if len(os.Args) != 3 {
		return errors.New("mode and path required")
	}
	mode, path := os.Args[1], os.Args[2]
	scanner := bufio.NewScanner(os.Stdin)
	if mode == "idle" {
		fmt.Println("idle")
		scanner.Scan()
		return scanner.Err()
	}
	if mode == "block" {
		fmt.Println("waiting")
		release, err := filelock.LockPath(path)
		if err != nil {
			return err
		}
		defer release()
		fmt.Println("acquired")
		scanner.Scan()
		return scanner.Err()
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		fmt.Println(outcome(err))
		return nil
	}
	defer f.Close()
	if mode == "probe" {
		fmt.Println(outcome(filelock.TryLock(f)))
		return nil
	}
	if err := filelock.Lock(f); err != nil {
		return err
	}
	fmt.Println("held")
	var child *exec.Cmd
	defer func() {
		if child != nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	for scanner.Scan() {
		switch scanner.Text() {
		case "unlock":
			fmt.Println(outcome(filelock.Unlock(f)))
		case "close":
			fmt.Println(outcome(f.Close()))
		case "spawn", "spawn-inherit":
			child = exec.Command(os.Args[0], "idle", path)
			if scanner.Text() == "spawn-inherit" {
				child.Stderr = f
			}
			input, err := child.StdinPipe()
			if err != nil {
				return err
			}
			defer input.Close()
			output, err := child.StdoutPipe()
			if err != nil {
				return err
			}
			if err := child.Start(); err != nil {
				return err
			}
			reader := bufio.NewScanner(output)
			if !reader.Scan() || reader.Text() != "idle" {
				return errors.New("child did not start")
			}
			fmt.Println("spawned")
		case "exit":
			return nil
		default:
			return errors.New("unknown process command")
		}
	}
	return scanner.Err()
}
