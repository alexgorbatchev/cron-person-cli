//go:build unix

package crontabreference

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoveredChild(t *testing.T) {
	operation := os.Getenv("CRONTAB_COVERED_CHILD")
	if operation == "" {
		t.Skip("subprocess protocol only")
	}
	var output io.Writer = os.Stdout
	if os.Getenv("CRONTAB_EXPECT_FAILURE") == "1" {
		output = failedWriter{}
	}
	code := serveChild(operation, os.Stdin, output)
	if (code == 2) != (os.Getenv("CRONTAB_EXPECT_FAILURE") == "1") {
		t.Fatalf("child protocol code %d", code)
	}
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestChildProtocol(t *testing.T) {
	if code := serveChild("read", failedReader{}, io.Discard); code != 2 {
		t.Fatalf("read error code %d", code)
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "crontab"), []byte("#!/bin/sh\nif [ \"$1\" = '-' ]; then /bin/cat > installed; else printf '\\377\\000\\015\\012'; fi\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := func(operation string) *exec.Cmd {
		args := []string{"-test.run=^TestCoveredChild$"}
		if counters := os.Getenv("CRONTAB_COUNTERS"); counters != "" {
			args = append(args, "-test.gocoverdir="+counters)
		}
		cmd := exec.Command(exe, args...)
		cmd.Env = []string{"PATH=" + directory, "CRONTAB_COVERED_CHILD=" + operation, "GORACE=atexit_sleep_ms=0"}
		return cmd
	}
	for _, operation := range []string{"read", "write"} {
		cmd := command(operation)
		cmd.Dir = directory
		cmd.Stdin = bytes.NewReader([]byte{0, 255, 13, 10})
		output, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var result [2]string
		if err := json.NewDecoder(bytes.NewReader(output)).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result[1] != "" {
			t.Fatal(result[1])
		}
		if operation == "read" && result[0] != "ff000d0a" {
			t.Fatal(result[0])
		}
	}
	installed, err := os.ReadFile(filepath.Join(directory, "installed"))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(installed) != "00ff0d0a" {
		t.Fatal(installed)
	}
	for _, failingOutput := range []bool{false, true} {
		cmd := command("read")
		cmd.Env = append(cmd.Env, "PATH="+directory+"/absent")
		if failingOutput {
			cmd.Env = append(cmd.Env, "CRONTAB_EXPECT_FAILURE=1")
			if err := cmd.Run(); err != nil {
				t.Fatalf("output failure %v", err)
			}
		} else if output, err := cmd.Output(); err != nil || !strings.Contains(string(output), "72656164696e67") {
			t.Fatalf("missing command %s: %v", output, err)
		}
	}
}

func TestRecordedReference(t *testing.T) {
	file, err := os.Open("../../crontab-rs/cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	decoder := json.NewDecoder(file)
	for number := 1; ; number++ {
		var c struct {
			Function string   `json:"fn"`
			Args     []string `json:"args"`
			Value    *string  `json:"ok"`
			Error    *string  `json:"error"`
		}
		if err := decoder.Decode(&c); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		var value string
		switch c.Function {
		case "Text":
			value, err = Text(c.Args[0], c.Args[1], c.Args[2])
		case "Process":
			value, err = Process(c.Args[0])
		default:
			t.Fatalf("case %d: unknown function %q", number, c.Function)
		}
		if c.Error != nil {
			if err == nil || err.Error() != *c.Error {
				t.Fatalf("case %d: error %v, want %q", number, err, *c.Error)
			}
		} else if c.Value == nil || err != nil || value != *c.Value {
			t.Fatalf("case %d: value %q, error %v, want %v", number, value, err, c.Value)
		}
	}
}

func TestDirectChildObservations(t *testing.T) {
	directory := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = '-' ]; then /bin/cat > installed; else printf '\\377\\000\\015\\012'; fi\n"
	if err := os.WriteFile(filepath.Join(directory, "crontab"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	t.Setenv("PATH", directory)
	for _, operation := range []string{"read", "write"} {
		got := child(operation, "contents")
		if got[1] != "" || operation == "read" && got[0] != "ff000d0a" {
			t.Fatalf("%s: %v", operation, got)
		}
	}
	if data, err := os.ReadFile("installed"); err != nil || string(data) != "contents" {
		t.Fatalf("installed %q: %v", data, err)
	}
	t.Setenv("PATH", directory+"/missing")
	if got := child("read", ""); got[1] == "" {
		t.Fatal("missing command was accepted")
	}
	if got := serveChild("read", strings.NewReader(""), failedWriter{}); got != 2 {
		t.Fatalf("failed writer code %d", got)
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
