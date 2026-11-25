//go:build !windows

package bootstrapreference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fixtureTargets(t *testing.T) (string, string) {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	scratch, err := os.MkdirTemp(filepath.Join(repo, ".tmp"), "bootstrap-go-tests-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(scratch); err != nil {
			t.Error(err)
		}
	})
	source, err := os.ReadFile(filepath.Join(repo, "scripts/dev-bootstrap.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(source), "func main()") != 1 {
		t.Fatal("original main signature changed")
	}
	if err := os.WriteFile(filepath.Join(scratch, "original.go"), bytes.Replace(source, []byte("func main()"), []byte("func originalMain()"), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := os.ReadFile("probe.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "probe.go"), probe, 0o600); err != nil {
		t.Fatal(err)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	main, helper := filepath.Join(scratch, "main"), filepath.Join(scratch, "probe")
	for _, args := range [][]string{{"build", "-o", main, filepath.Join(repo, "scripts/dev-bootstrap.go")}, {"build", "-o", helper, filepath.Join(scratch, "original.go"), filepath.Join(scratch, "probe.go")}} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, goBin, args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off", "GOTMPDIR="+scratch)
		configure(cmd)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("build actual helper: %v: %s", err, out)
		}
	}
	return main, helper
}

func TestConfiguredCancellationStopsOwnedProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sleep", "30")
	configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("cancelled process succeeded")
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() && cmd.ProcessState.String() != "signal: killed" {
		t.Fatalf("process was not reaped: %v", cmd.ProcessState)
	}
}

func TestOriginalCorpusInOwnPackage(t *testing.T) {
	main, helper := fixtureTargets(t)
	t.Setenv("BOOTSTRAP_GO_MAIN", main)
	t.Setenv("BOOTSTRAP_GO_PROBE", helper)
	file, err := os.Open(filepath.Join("..", "..", "bootstrap-rs", "cases.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	count := 0
	for scanner.Scan() {
		var record struct {
			Args  []string `json:"args"`
			OK    string   `json:"ok"`
			Error string   `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if len(record.Args) != 1 {
			t.Fatal("invalid recorded arguments")
		}
		value, err := Scenario(record.Args[0])
		if record.Error != "" {
			if err == nil || err.Error() != record.Error || value != "" {
				t.Fatalf("case %d: got (%q, %v), expected error %q", count, value, err, record.Error)
			}
		} else if err != nil {
			t.Fatalf("case %d: %v", count, err)
		}
		if value != record.OK {
			t.Fatalf("case %d differs from actual Go recording", count)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 169 {
		t.Fatalf("unexpected count %d", count)
	}
}

func TestFixtureValidationAndProtocolFailures(t *testing.T) {
	for _, input := range []string{"not JSON", `{}`, `{"op":"main","source":"../outside"}`, `{"op":"main","source":"/outside"}`, `{"op":"main","dotfiles":"outside"}`, `{"op":"main","out":"xy"}`, `{"op":"main","err":"xy"}`, `{"op":"main","status":256}`, `{"op":"main","status":-1}`, `{"op":"main","source":"bin/\u0000invalid"}`, `{"op":"main","source":"` + strings.Repeat("x", 256) + `"}`} {
		if _, err := Scenario(input); err == nil {
			t.Errorf("accepted invalid fixture %s", input)
		}
	}
	t.Setenv("BOOTSTRAP_GO_MAIN", "relative-target")
	if _, err := Scenario(`{"op":"main"}`); err == nil {
		t.Fatal("accepted relative target")
	}
	for _, script := range []string{"exit 7", "printf 'bad protocol'", `printf '%s' '["not hex",""]'`, `mkdir "$EVENTS"; printf '%s' '["",""]'`} {
		dir := t.TempDir()
		target := filepath.Join(dir, "probe")
		text := "#!/bin/sh\n" + strings.ReplaceAll(script, "mkdir ", "/bin/mkdir ") + "\n"
		if err := os.WriteFile(target, []byte(text), 0o700); err != nil {
			t.Fatal(err)
		}
		op := "command"
		if strings.HasPrefix(script, "exit") {
			op = "main"
		}
		_, err := scenario(fmt.Sprintf(`{"op":%q}`, op), target)
		if strings.HasPrefix(script, "exit") {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatalf("accepted broken probe %s", script)
		}
	}
	if _, err := scenario(`{"op":"main"}`, "/missing-bootstrap-probe"); err == nil {
		t.Fatal("accepted missing target")
	}
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := write(filepath.Join(blocker, "child"), "data", 0o600); err == nil {
		t.Fatal("accepted non-directory parent")
	}
	input, err := os.Open(blocker)
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	var output, diagnostics bytes.Buffer
	if status := serve(input, &output, &diagnostics, ""); status != 0 || !strings.Contains(output.String(), "file already closed") {
		t.Fatalf("reader failure: %d %s", status, &output)
	}
	if status := serve(strings.NewReader("invalid"), input, &diagnostics, ""); status != 2 || diagnostics.Len() == 0 {
		t.Fatalf("writer failure: %d %s", status, &diagnostics)
	}
	output.Reset()
	if status := serve(strings.NewReader("invalid"), &output, &diagnostics, ""); status != 0 || !strings.Contains(output.String(), "invalid scenario JSON") {
		t.Fatalf("scenario error: %d %s", status, &output)
	}
}
