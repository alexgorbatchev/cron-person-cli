package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v3"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	terminatePrompt = "\nAlso terminate the task? [y/N] "
	// inputBacklog bounds input lines queued for a task that is not reading.
	inputBacklog = 64
)

// runAttached runs a manual task in the foreground. The run is delegated to a
// recorder, `cron-person exec`, started in its own session so the terminal's
// Ctrl+C reaches only this process, and the recorder keeps capturing the run if
// this process detaches.
func runAttached(cmd *cobra.Command, dir, command string) error {
	st, err := getStore(cmd)
	if err != nil {
		return err
	}
	configPath, err := getConfigPath(cmd)
	if err != nil {
		return err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving directory %s: %w", dir, err)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the cron-person executable: %w", err)
	}
	idFile, err := os.CreateTemp("", "cron-person-run-*.id")
	if err != nil {
		return fmt.Errorf("creating run ID file: %w", err)
	}
	idPath := idFile.Name()
	if err := idFile.Close(); err != nil {
		return fmt.Errorf("creating run ID file: %w", err)
	}
	defer os.Remove(idPath)

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	a := attachedRun{
		stdin:       cmd.InOrStdin(),
		stdout:      cmd.OutOrStdout(),
		stderr:      cmd.ErrOrStderr(),
		interactive: isTerminal(cmd.InOrStdin()),
		signals:     signals,
		idPath:      idPath,
	}
	args := []string{"--store", st.Path(), "--config", configPath, "exec",
		"--" + triggerFlag, "manual", "--" + runIDFileFlag, idPath, absDir, "--", command}
	return a.run(exec.Command(exe, args...))
}

// attachedRun relays a recorder's input and output. With interactive input it
// forwards typed lines to the task and answers Ctrl+C with a prompt: yes, or a
// second Ctrl+C, terminates the task; anything else detaches and leaves it
// running. Without interactive input, and for SIGTERM or SIGHUP, it terminates.
type attachedRun struct {
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	interactive bool
	signals     <-chan os.Signal
	idPath      string
}

func (a attachedRun) run(child *exec.Cmd) error {
	child.SysProcAttr = recorderProcAttr()
	outR, outW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("creating output pipe: %w", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		return errors.Join(fmt.Errorf("creating output pipe: %w", err), outR.Close(), outW.Close())
	}
	child.Stdout, child.Stderr = outW, errW

	// A file is handed to the recorder as is. Anything else, including a
	// terminal, reaches it through a pipe this process copies into: a detached
	// task must not keep reading the terminal, and os/exec would otherwise wait
	// on its own copier for input that may never end.
	var inR, inW *os.File
	if f, ok := a.stdin.(*os.File); ok && !a.interactive {
		child.Stdin = f
	} else {
		if inR, inW, err = os.Pipe(); err != nil {
			return errors.Join(fmt.Errorf("creating input pipe: %w", err), outR.Close(), outW.Close(), errR.Close(), errW.Close())
		}
		child.Stdin = inR
	}
	startErr := child.Start()
	for _, f := range []*os.File{outW, errW, inR} {
		if f != nil {
			_ = f.Close()
		}
	}
	if startErr != nil {
		_, _ = outR.Close(), errR.Close()
		if inW != nil {
			_ = inW.Close()
		}
		return fmt.Errorf("starting task recorder: %w", startErr)
	}

	var relays sync.WaitGroup
	mu := &sync.Mutex{}
	for _, r := range []struct {
		dst io.Writer
		src *os.File
	}{{a.stdout, outR}, {a.stderr, errR}} {
		relays.Add(1)
		go func() {
			defer relays.Done()
			_, _ = io.Copy(&serialWriter{mu: mu, w: r.dst}, r.src)
		}()
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	var lines <-chan []byte
	var forward chan<- []byte
	switch {
	case a.interactive:
		lines = readInput(a.stdin)
		forward = forwardInput(inW)
	case inW != nil:
		go func() {
			_, _ = io.Copy(inW, a.stdin)
			_ = inW.Close()
		}()
	}
	closeInput := func() {
		if forward != nil {
			close(forward)
			forward = nil
		}
	}
	defer closeInput()

	prompting := false
	for {
		select {
		case err := <-exited:
			relays.Wait()
			_, _ = outR.Close(), errR.Close()
			if prompting {
				fmt.Fprintln(a.stderr)
			}
			return recorderResult(err)
		case data, ok := <-lines:
			switch {
			case prompting:
				prompting = false
				if ok && isYes(data) {
					a.terminate(child)
					continue
				}
				closeInput()
				_, _ = outR.Close(), errR.Close()
				relays.Wait()
				return a.detached()
			case !ok:
				lines = nil
				closeInput()
			case data == nil:
				closeInput()
			case forward != nil:
				forward <- data
			}
		case sig := <-a.signals:
			if sig == os.Interrupt && lines != nil && !prompting {
				prompting = true
				fmt.Fprint(a.stderr, terminatePrompt)
				continue
			}
			prompting = false
			a.terminate(child)
		}
	}
}

func (a attachedRun) terminate(child *exec.Cmd) {
	if err := interruptRecorder(child.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
		fmt.Fprintf(a.stderr, "terminating task: %v\n", err)
	}
}

func (a attachedRun) detached() error {
	data, _ := os.ReadFile(a.idPath)
	id := strings.TrimSpace(string(data))
	switch {
	case cobrahelptree.IsAgentMode():
		fmt.Fprintf(a.stderr, "detached: true\nrun_id: %s\n", id)
	case id == "":
		fmt.Fprintln(a.stderr, "[INFO] The task keeps running in the background. Find it with: cron-person task history")
	default:
		fmt.Fprintf(a.stderr, "[INFO] The task keeps running in the background as run %s\n", id)
		fmt.Fprintf(a.stderr, "       Follow it with: cron-person task log --follow %s\n", id)
	}
	return nil
}

// recorderResult maps the recorder's exit to this command's result. The
// recorder has already reported any failure on the relayed stderr.
func recorderResult(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
		return exitStatus(exitErr.ExitCode())
	}
	if err != nil {
		return fmt.Errorf("task recorder failed: %w", err)
	}
	return nil
}

// readInput delivers input as it is read, a nil slice for each end of input,
// and closes the channel when no more can arrive. A terminal can be read again
// after Ctrl+D, so only two consecutive ends of input stop reading.
func readInput(r io.Reader) <-chan []byte {
	ch := make(chan []byte, inputBacklog)
	go func() {
		defer close(ch)
		buf := make([]byte, 4096)
		ended := false
		for {
			n, err := r.Read(buf)
			if n > 0 {
				ended = false
				ch <- bytes.Clone(buf[:n])
			}
			switch {
			case errors.Is(err, io.EOF) && n == 0:
				if ended {
					return
				}
				ended = true
				ch <- nil
			case err != nil && !errors.Is(err, io.EOF):
				return
			}
		}
	}()
	return ch
}

// forwardInput writes queued input to the task and closes its input once the
// queue is closed, without blocking the signal loop on a task that is not
// reading.
func forwardInput(w io.WriteCloser) chan<- []byte {
	ch := make(chan []byte, inputBacklog)
	go func() {
		broken := false
		for data := range ch {
			if !broken {
				_, err := w.Write(data)
				broken = err != nil
			}
		}
		_ = w.Close()
	}()
	return ch
}

func isYes(answer []byte) bool {
	switch strings.ToLower(strings.TrimSpace(string(answer))) {
	case "y", "yes":
		return true
	}
	return false
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// serialWriter keeps the stdout and stderr relays from writing to a shared
// destination at the same time.
type serialWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w *serialWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}
