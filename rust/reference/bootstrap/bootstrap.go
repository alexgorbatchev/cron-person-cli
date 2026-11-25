// Package bootstrapreference observes the real development bootstrap program in isolated fixtures.
package bootstrapreference

import (
	"context"
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
	"time"
)

type request struct {
	Op               string   `json:"op"`
	Mode             string   `json:"mode"`
	Source           string   `json:"source"`
	Dotfiles         string   `json:"dotfiles"`
	Out              string   `json:"out"`
	Err              string   `json:"err"`
	Status           int      `json:"status"`
	Help             string   `json:"help"`
	HelpStatus       int      `json:"help_status"`
	RegisterStatus   int      `json:"register_status"`
	WhichStatus      int      `json:"which_status"`
	Destination      string   `json:"destination"`
	NoRelease        bool     `json:"no_release"`
	Unset            bool     `json:"unset"`
	RemoveCwd        bool     `json:"remove_cwd"`
	AbsoluteSource   bool     `json:"absolute_source"`
	AbsoluteDotfiles bool     `json:"absolute_dotfiles"`
	Args             []string `json:"args"`
}

func init() {
	if os.Getenv("BOOTSTRAP_FIXTURE_RUN") != "1" {
		return
	}
	os.Exit(serve(os.Stdin, os.Stdout, os.Stderr, os.Getenv("BOOTSTRAP_TARGET")))
}

func serve(input io.Reader, output, diagnostics io.Writer, target string) int {
	data, err := io.ReadAll(input)
	value := ""
	if err == nil {
		value, err = scenario(string(data), target)
	}
	result := struct {
		Value string `json:"value"`
		Error string `json:"error"`
	}{Value: value}
	if err != nil {
		result.Error = err.Error()
	}
	if err := json.NewEncoder(output).Encode(result); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 2
	}
	return 0
}

func octal(data []byte) string {
	var s strings.Builder
	for _, b := range data {
		fmt.Fprintf(&s, "\\%03o", b)
	}
	return s.String()
}

func write(path, data string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(data), mode)
}

// Scenario invokes an unchanged Go main or a probe linked to its actual unexported functions.
// No bootstrap decisions are reproduced here; only fixture setup and observations live here.
func Scenario(input string) (string, error) { return scenario(input, "") }

func scenario(input, target string) (value string, err error) {
	var req request
	if json.Unmarshal([]byte(input), &req) != nil {
		return "", errors.New("invalid scenario JSON")
	}
	if runtime.GOOS == "windows" {
		return "", errors.New("shell fixtures require Unix")
	}
	if req.Op != "command" && req.Op != "bootstrap" && req.Op != "run" && req.Op != "main" {
		return "", errors.New("invalid scenario operation")
	}
	if filepath.IsAbs(req.Source) || strings.Contains(req.Source, "..") {
		return "", errors.New("source must stay inside fixture")
	}
	if req.Dotfiles != "" && req.Dotfiles != "missing" && req.Dotfiles != "file" {
		return "", errors.New("invalid dotfiles fixture")
	}
	out, err := hex.DecodeString(req.Out)
	if err != nil {
		return "", errors.New("invalid stdout hex")
	}
	stderr, err := hex.DecodeString(req.Err)
	if err != nil {
		return "", errors.New("invalid stderr hex")
	}
	for _, status := range []int{req.Status, req.HelpStatus, req.RegisterStatus, req.WhichStatus} {
		if status < 0 || status > 255 {
			return "", errors.New("invalid fixture status")
		}
	}
	root, err := os.MkdirTemp("", "bootstrap-reference-")
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(root)) }()
	cwd := filepath.Join(root, "repo")
	dotfiles := filepath.Join(root, "dotfiles with spaces")
	commands := filepath.Join(root, "commands")
	home := filepath.Join(root, "home")
	for _, dir := range []string{cwd, dotfiles, commands, home} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return "", err
		}
	}
	source := filepath.Join(cwd, "bin", "cron-person")
	if req.Source != "" {
		source = filepath.Join(cwd, req.Source)
	}
	log := filepath.Join(root, "events")
	trace := "printf '%s\\000' \"$0\" \"$PWD\" \"$HOME\" \"$PATH\" \"$#\" \"$@\" >> \"$EVENTS\"\nprintf '\\000' >> \"$EVENTS\"\n"
	sourceScript := "#!/bin/sh\n" + trace + fmt.Sprintf("printf '%%b' '%s'\nprintf '%%b' '%s' >&2\nexit %d\n", octal(out), octal(stderr), req.Status)
	mode := os.FileMode(0o700)
	switch req.Mode {
	case "empty":
		sourceScript = ""
	case "format":
		sourceScript = "not an executable\n"
	case "interpreter":
		sourceScript = "#!/missing-bootstrap-interpreter\n"
	case "denied":
		mode = 0o600
	case "signal":
		sourceScript = "#!/bin/sh\n" + trace + "kill -TERM $$\n"
	}
	if req.Mode == "directory" {
		if err := os.MkdirAll(source, 0o700); err != nil {
			return "", err
		}
	} else if req.Mode != "missing-source" {
		if err := write(source, sourceScript, mode); err != nil {
			return "", err
		}
	}
	if req.Mode == "symlink" {
		if err := os.Rename(source, source+"-target"); err != nil {
			return "", err
		}
		if err := os.Symlink(source+"-target", source); err != nil {
			return "", err
		}
	}
	help := req.Help
	if help == "" {
		help = "tool dev use --bin name=source\n"
	}
	destination := req.Destination
	if destination == "" {
		destination = dotfiles + "/.state/custom-dev-tools/cron-person\n"
	}
	destination = strings.ReplaceAll(destination, "<root>", root)
	fake := "#!/bin/sh\nset -eu\n" + trace + fmt.Sprintf(`if [ "$4" = '--help' ]; then
printf '%%b' '%s'
printf '%%b' '%s' >&2
exit %d
elif [ "$2" = 'dev' ]; then
printf '%%b' '%s' >&2
if [ %d -ne 0 ]; then exit %d; fi
/bin/mkdir -p .state/custom-dev-tools
/bin/cp "${6#cron-person=}" .state/custom-dev-tools/cron-person
printf '%%s\000' "$@" > registration
elif [ "$2" = 'which' ]; then
printf '%%b' '%s'
printf '%%b' '%s' >&2
exit %d
fi
`, octal([]byte(help)), octal(stderr), req.HelpStatus, octal(stderr), req.RegisterStatus, req.RegisterStatus, octal([]byte(destination)), octal(stderr), req.WhichStatus)
	if req.Mode != "missing-cli" {
		if err := write(filepath.Join(commands, "dotfiles"), fake, 0o700); err != nil {
			return "", err
		}
	}
	release := filepath.Join(dotfiles, ".generated/binaries/cron-person/v1/cron-person")
	if !req.NoRelease {
		if err := write(release, "managed release\x00\xff", 0o700); err != nil {
			return "", err
		}
	}
	if req.Mode == "existing-override" {
		if err := write(filepath.Join(dotfiles, ".state/custom-dev-tools/cron-person"), "old override", 0o700); err != nil {
			return "", err
		}
	}
	probe := map[string]any{"Op": req.Op, "Source": source, "Dotfiles": dotfiles, "Directory": dotfiles, "Executable": source, "Args": req.Args, "RemoveCwd": req.RemoveCwd}
	if !req.AbsoluteSource {
		probe["Source"], err = filepath.Rel(cwd, source)
		if err != nil {
			return "", err
		}
	}
	if !req.AbsoluteDotfiles {
		probe["Dotfiles"], err = filepath.Rel(cwd, dotfiles)
		if err != nil {
			return "", err
		}
	}
	if req.Dotfiles == "missing" {
		probe["Dotfiles"] = filepath.Join(root, "missing")
	}
	if req.Dotfiles == "file" {
		probe["Dotfiles"] = source
	}
	if req.Mode == "bad-cwd" {
		probe["Directory"] = filepath.Join(root, "missing")
	}
	if req.Mode == "lookup" || req.Mode == "relative-path" {
		probe["Executable"] = "dotfiles"
	}
	if req.Mode == "nul" {
		probe["Executable"] = "bad\x00executable"
	}
	if req.Mode == "relative-executable" {
		probe["Executable"] = "bin/cron-person"
	}
	if req.RemoveCwd {
		cwd = filepath.Join(root, "removed")
		if err := os.Mkdir(cwd, 0o700); err != nil {
			return "", err
		}
	}
	if target == "" {
		key := "BOOTSTRAP_GO_PROBE"
		if req.Op == "main" {
			key = "BOOTSTRAP_GO_MAIN"
		}
		target = os.Getenv(key)
	}
	if !filepath.IsAbs(target) {
		return "", errors.New("fixture target must be absolute")
	}
	data, err := json.Marshal(probe)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, target)
	if req.Op == "main" {
		cmd.Args = append(cmd.Args, req.Args...)
	}
	cmd.Dir = cwd
	path := commands
	if req.Mode == "relative-path" {
		path = "../commands"
	}
	cmd.Env = []string{"HOME=" + home, "PATH=" + path, "EVENTS=" + log}
	if !req.Unset {
		cmd.Env = append(cmd.Env, "dotfiles_dir="+probe["Dotfiles"].(string))
	}
	if req.Op != "main" {
		cmd.Env = append(cmd.Env, "BOOTSTRAP_PROBE=1")
	}
	if profile := os.Getenv("LLVM_PROFILE_FILE"); profile != "" {
		cmd.Env = append(cmd.Env, "LLVM_PROFILE_FILE="+profile)
	}
	cmd.Stdin = strings.NewReader(string(data))
	configure(cmd)
	var childErr strings.Builder
	cmd.Stderr = &childErr
	output, runErr := cmd.Output()
	status := 0
	if runErr != nil {
		var failure *exec.ExitError
		if !errors.As(runErr, &failure) {
			return "", runErr
		}
		status = failure.ExitCode()
	}
	normalize := func(data []byte) string {
		return hex.EncodeToString([]byte(strings.ReplaceAll(string(data), root, "<fixture>")))
	}
	observations := []any{normalize(output), normalize([]byte(childErr.String())), status}
	var printed []byte
	if req.Op != "main" {
		text := strings.TrimSuffix(string(output), "\n")
		if i := strings.LastIndexByte(text, '\n'); i >= 0 {
			printed = []byte(text[:i+1])
			output = []byte(text[i+1:])
		}
		var result [2]string
		if err := json.Unmarshal(output, &result); err != nil {
			return "", fmt.Errorf("probe output %q: %w", output, err)
		}
		for i, s := range result {
			raw, err := hex.DecodeString(s)
			if err != nil {
				return "", err
			}
			result[i] = normalize(raw)
		}
		observations[0] = result
	}
	for _, path := range []string{log, release, filepath.Join(dotfiles, "registration"), filepath.Join(dotfiles, ".state/custom-dev-tools/cron-person")} {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			observations = append(observations, nil)
		} else if err != nil {
			return "", err
		} else {
			observations = append(observations, normalize(data))
		}
	}
	observations = append(observations, normalize(printed))
	encoded, err := json.Marshal(observations)
	return string(encoded), err
}
