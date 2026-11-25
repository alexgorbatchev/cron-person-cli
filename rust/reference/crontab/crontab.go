// Package crontabreference characterizes the actual production crontab APIs.
package crontabreference

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/alexgorbatchev/cron-person-cli/internal/crontab"
)

// Text preserves arbitrary Go string bytes across the scalar JSON bridge.
func Text(operation, existing, managed string) (string, error) {
	data, err := hex.DecodeString(existing)
	if err != nil {
		return "", errors.New("invalid existing hex")
	}
	block, err := hex.DecodeString(managed)
	if err != nil {
		return "", errors.New("invalid managed hex")
	}
	var output string
	switch operation {
	case "remove":
		output, err = crontab.RemoveBlock(string(data))
	case "merge":
		output, err = crontab.MergeCrontab(string(data), string(block))
	default:
		return "", errors.New("unknown text operation")
	}
	return hex.EncodeToString([]byte(output)), err
}

type request struct {
	Operation string `json:"operation"`
	Mode      string `json:"mode"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Content   string `json:"content"`
	Status    int    `json:"status"`
}

// The probe re-executes itself, so PATH and cwd never mutate the calling process.
func init() {
	if operation := os.Getenv("CRONTAB_REFERENCE_CHILD"); operation != "" {
		os.Exit(serveChild(operation, os.Stdin, os.Stdout))
	}
}

func serveChild(operation string, input io.Reader, output io.Writer) int {
	content, err := io.ReadAll(input)
	if err != nil {
		return 2
	}
	if err := json.NewEncoder(output).Encode(child(operation, string(content))); err != nil {
		return 2
	}
	return 0
}

func child(operation, content string) [2]string {
	var data string
	var err error
	if operation == "read" {
		data, err = crontab.ReadSystemCrontab()
	} else {
		err = crontab.WriteSystemCrontab(content)
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	return [2]string{hex.EncodeToString([]byte(data)), hex.EncodeToString([]byte(message))}
}

func octal(data []byte) string {
	var output strings.Builder
	for _, b := range data {
		fmt.Fprintf(&output, "\\%03o", b)
	}
	return output.String()
}

// Process executes only an invocation-owned fake crontab and returns byte observations.
// Only the random fixture prefix is normalized; diagnostics and bytes stay intact.
func Process(input string) (string, error) {
	var req request
	if json.Unmarshal([]byte(input), &req) != nil {
		return "", errors.New("invalid process request")
	}
	if req.Operation != "read" && req.Operation != "write" {
		return "", errors.New("unknown process operation")
	}
	if req.Status < 0 || req.Status > 255 {
		return "", errors.New("invalid process status")
	}
	if runtime.GOOS == "windows" {
		return "", errors.New("process fixtures require Unix")
	}
	stdout, err := hex.DecodeString(req.Stdout)
	if err != nil {
		return "", errors.New("invalid stdout hex")
	}
	stderr, err := hex.DecodeString(req.Stderr)
	if err != nil {
		return "", errors.New("invalid stderr hex")
	}
	content, err := hex.DecodeString(req.Content)
	if err != nil {
		return "", errors.New("invalid content hex")
	}
	if !strings.Contains("|normal|missing|denied|format|interpreter|relative|closed|signal|", "|"+req.Mode+"|") {
		return "", errors.New("unknown process mode")
	}
	dir, err := os.MkdirTemp("", "crontab-reference-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir) // Invocation-owned fixture; no process survives Output below.
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$#\" \"$@\" > calls\nif [ \"$1\" = '-' ]; then /bin/cat > installed; fi\nprintf '%%b' '%s'\nprintf '%%b' '%s' >&2\nexit %d\n", octal(stdout), octal(stderr), req.Status)
	if req.Mode == "closed" {
		script = strings.Replace(script, "/bin/cat > installed", "exec 0<&-", 1)
	}
	if req.Mode == "signal" {
		script = strings.Replace(script, fmt.Sprintf("exit %d", req.Status), "kill -TERM $$", 1)
	}
	if req.Mode == "format" {
		script = "invalid executable\n"
	}
	if req.Mode == "interpreter" {
		script = "#!/nonexistent-crontab-interpreter\n"
	}
	if req.Mode != "missing" {
		mode := os.FileMode(0o700)
		if req.Mode == "denied" {
			mode = 0o600
		}
		if err := os.WriteFile(filepath.Join(dir, "crontab"), []byte(script), mode); err != nil {
			return "", err
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(exe)
	cmd.Dir = dir
	path := dir
	if req.Mode == "relative" {
		path = "."
	}
	// The fixture finishes all I/O synchronously; race-instrumented children need no exit delay.
	cmd.Env = []string{"PATH=" + path, "HOME=" + dir, "CRONTAB_REFERENCE_CHILD=" + req.Operation, "GORACE=atexit_sleep_ms=0"}
	if coverage := os.Getenv("GOCOVERDIR"); coverage != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+coverage)
	}
	cmd.Stdin = strings.NewReader(string(content))
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var observed [2]string
	if err := json.Unmarshal(output, &observed); err != nil {
		return "", err
	}
	message, err := hex.DecodeString(observed[1])
	if err != nil {
		return "", err
	}
	observed[1] = hex.EncodeToString([]byte(strings.ReplaceAll(string(message), dir, "<fixture>")))
	files := [2]string{}
	for i, name := range []string{"calls", "installed"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		files[i] = hex.EncodeToString(data)
	}
	result, err := json.Marshal([4]string{observed[0], observed[1], files[0], files[1]})
	return string(result), err
}
