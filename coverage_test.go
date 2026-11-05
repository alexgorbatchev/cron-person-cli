package cronperson_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func coverageFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"justfile", "scripts/check-coverage.sh", "go.mod", "go.sum", ".testcoverage.yml"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		writeCoverageFile(t, dir, name, string(data))
	}
	writeCoverageFile(t, dir, ".tmp/another-session/keep", "sentinel")
	writeCoverageFile(t, dir, "value.go", "package fixture\nfunc value() int { return 1 }\n")
	writeCoverageFile(t, dir, "value_test.go", `package fixture
import ("testing"; "os"; "time"; "encoding/json"; "os/signal"; "syscall"; "os/exec")
func TestValue(t *testing.T) {
 if os.Getenv("FIXTURE_MODE") != "low" && value() != 1 { t.Fatal("wrong value") }
 if os.Getenv("FIXTURE_MODE") == "fail" { t.Fatal("intentional failure") }
 if os.Getenv("FIXTURE_MODE") == "cover failure" { if err := os.WriteFile(".testcoverage.yml", []byte("threshold: ["), 0600); err != nil { t.Fatal(err) } }
 if ready := os.Getenv("FIXTURE_READY"); ready != "" {
  if os.Getenv("FIXTURE_IGNORE_TERM") == "1" && os.Getenv("FIXTURE_DETACHED") != "1" {
   child := exec.Command(os.Args[0], "-test.run=^TestValue$")
   child.Env = append(os.Environ(), "FIXTURE_DETACHED=1")
   child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
   if err := child.Run(); err != nil { t.Fatal(err) }
   return
  }
  if os.Getenv("FIXTURE_IGNORE_TERM") == "1" { signal.Ignore(syscall.SIGTERM) }
  data, err := json.Marshal(struct { PID int; Scratch string }{os.Getpid(), os.TempDir()})
  if err != nil { t.Fatal(err) }
  if err := os.WriteFile(ready, data, 0600); err != nil { t.Fatal(err) }
  for { if _, err := os.Stat(os.Getenv("FIXTURE_RELEASE")); err == nil { break }; time.Sleep(10 * time.Millisecond) }
 }
}
`)
	return dir
}

func writeCoverageFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertCoverageClean(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, ".tmp"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "another-session" {
		t.Fatalf("coverage left scratch or removed another session: %v, %v", entries, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, ".tmp/another-session/keep")); err != nil || string(data) != "sentinel" {
		t.Fatalf("coverage damaged another session: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "coverage.out")); !os.IsNotExist(err) {
		t.Fatalf("coverage left shared profile: %v", err)
	}
}

func TestCoverageCleansAfterConsumers(t *testing.T) {
	for _, mode := range []string{"success", "fail", "low", "timeout", "cover failure"} {
		t.Run(mode, func(t *testing.T) {
			dir := coverageFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "just", "coverage")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "FIXTURE_MODE="+mode)
			if mode == "timeout" {
				cmd.Env = append(cmd.Env, "GOFLAGS=-timeout=500ms", "FIXTURE_READY="+filepath.Join(dir, "ready"))
			}
			out, err := cmd.CombinedOutput()
			if (err != nil) != (mode != "success") {
				t.Fatalf("coverage error = %v in %s\n%s", err, mode, out)
			}
			if mode == "low" && !strings.Contains(string(out), "threshold") {
				t.Fatalf("coverage gate did not evaluate profile:\n%s", out)
			}
			if mode == "timeout" && !strings.Contains(string(out), "test timed out") {
				t.Fatalf("test did not time out:\n%s", out)
			}
			assertCoverageClean(t, dir)
		})
	}
}

type coverageRun struct {
	cmd    *exec.Cmd
	out    bytes.Buffer
	cancel context.CancelFunc
	ready  string
	done   bool
}

func startCoverage(t *testing.T, dir, name string, ignoreTERM bool) *coverageRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	run := &coverageRun{cancel: cancel, ready: filepath.Join(dir, name+".ready")}
	run.cmd = exec.CommandContext(ctx, "bash", "scripts/check-coverage.sh")
	run.cmd.Dir = dir
	run.cmd.Env = append(os.Environ(), "FIXTURE_READY="+run.ready, "FIXTURE_RELEASE="+filepath.Join(dir, name+".release"))
	if ignoreTERM {
		run.cmd.Env = append(run.cmd.Env, "FIXTURE_IGNORE_TERM=1")
	}
	run.cmd.Stdout, run.cmd.Stderr = &run.out, &run.out
	run.cmd.Cancel = func() error { return run.cmd.Process.Signal(syscall.SIGTERM) }
	run.cmd.WaitDelay = 15 * time.Second
	if err := run.cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		if !run.done {
			// Cancellation is expected when an assertion fails before the normal wait.
			_ = run.cmd.Wait()
		}
	})
	return run
}

func (r *coverageRun) wait(t *testing.T, wantFailure bool) {
	t.Helper()
	err := r.cmd.Wait()
	r.done = true
	if (err != nil) != wantFailure {
		t.Fatalf("coverage error = %v, want failure %t\n%s", err, wantFailure, r.out.String())
	}
}

type coverageProcess struct {
	PID     int
	Scratch string
}

func (r *coverageRun) process(t *testing.T) coverageProcess {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(r.ready); err == nil {
			var process coverageProcess
			if json.Unmarshal(data, &process) == nil && process.PID > 0 && process.Scratch != "" {
				return process
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("coverage test did not become ready")
	return coverageProcess{}
}

func TestCoverageCancellationPreservesConcurrentRun(t *testing.T) {
	for _, mode := range []string{"graceful", "forced"} {
		t.Run(mode, func(t *testing.T) {
			dir := coverageFixture(t)
			first := startCoverage(t, dir, "first", mode == "forced")
			second := startCoverage(t, dir, "second", false)
			firstProcess, secondProcess := first.process(t), second.process(t)
			if firstProcess.Scratch == secondProcess.Scratch {
				t.Fatal("concurrent coverage runs share scratch")
			}
			first.cancel()
			first.wait(t, true)
			assertCoverageProcessStopped(t, firstProcess)
			if _, err := os.Stat(firstProcess.Scratch); !os.IsNotExist(err) {
				t.Fatalf("cancelled run retained scratch: %v", err)
			}
			if _, err := os.Stat(secondProcess.Scratch); err != nil {
				t.Fatalf("cancellation removed concurrent run's scratch: %v", err)
			}
			writeCoverageFile(t, dir, "second.release", "ready")
			second.wait(t, false)
			assertCoverageProcessStopped(t, secondProcess)
			assertCoverageClean(t, dir)
		})
	}
}

func assertCoverageProcessStopped(t *testing.T, process coverageProcess) {
	t.Helper()
	out, err := exec.Command("ps", "-p", strconv.Itoa(process.PID), "-o", "stat=").Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return // ps returns 1 when this process no longer exists.
		}
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "Z") {
		t.Fatalf("coverage test process %d survived cleanup: %s", process.PID, out)
	}
}
