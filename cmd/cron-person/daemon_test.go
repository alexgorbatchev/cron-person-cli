//go:build daemon

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
)

const (
	daemonUser    = "tester"
	daemonProject = "/home/tester/project"
	daemonOutput  = "/home/tester/out"
	daemonBin     = "/opt/cron-person/cron-person"
	movedBin      = "/opt/cron-person-moved/cron-person"
	personalJob   = "@yearly personal-job"
	// Cron fires on minute boundaries, so one fire can take a full minute
	// plus the daemon's own scan delay.
	fireTimeout = 150 * time.Second
)

// TestDaemonRunsGeneratedEntries installs generated entries into a real
// Debian cron daemon inside a disposable container and observes what the
// daemon actually runs. Unit tests stay hermetic; this one runs only with
// `just test-daemon` and never touches the host crontab.
func TestDaemonRunsGeneratedEntries(t *testing.T) {
	// No skip without Docker: this test only runs when asked for, so a
	// missing daemon must fail it rather than pass it vacuously.
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()

	ctr, err := testcontainers.Run(ctx, "", testcontainers.WithDockerfile(testcontainers.FromDockerfile{
		Context:    filepath.Join("..", "..", "testdata", "daemon"),
		Dockerfile: "Dockerfile",
	}))
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}
	d := &daemon{t: t, ctx: ctx, ctr: ctr, bin: daemonBin}

	d.install(buildLinuxBinary(t, "0.0.0-daemon"), daemonBin)
	d.asUser(`mkdir -p ` + daemonProject + ` && cat > ` + daemonProject + `/.cronrc <<'EOF'
GREETING=from-cronrc
* * * * * printf '%s|%s|%s\n' "$PWD" "$GREETING" '100%' >> ` + daemonOutput + `
EOF
printf '%s\n' '` + personalJob + `' | crontab -`)
	d.asUser(daemonBin + " allow " + daemonProject + " && " + daemonBin + " crontab sync")

	start := time.Now()
	// A run is recorded as running until its command exits, so wait for the
	// outcome rather than for the command's output.
	d.waitFor("first scheduled run", func() bool {
		return d.outputLines() >= 1 && strings.Contains(d.history()[0], "status: succeeded")
	})
	t.Logf("first fire after %s", time.Since(start).Round(time.Second))
	if got := d.asUser("head -n 1 " + daemonOutput); got != daemonProject+"|from-cronrc|100%\n" {
		t.Fatalf("scheduled run wrote %q, want the project directory, .cronrc environment, and a literal percent sign", got)
	}
	if runs := d.history(); !strings.Contains(runs[0], "status: succeeded") || !strings.Contains(runs[0], "trigger: cron") {
		t.Fatalf("history does not record the scheduled run: %q", runs[0])
	}

	// Editing .cronrc without approving it blocks the next fire.
	d.asUser("printf '# edited\\n' >> " + daemonProject + "/.cronrc")
	before, start := d.outputLines(), time.Now()
	d.waitFor("tampered run to be refused", func() bool { return d.blockedRuns() >= 1 })
	t.Logf("tampered fire refused after %s", time.Since(start).Round(time.Second))
	d.requireBlockedReason("changed")
	if got := d.outputLines(); got != before {
		t.Fatalf("tampered entry ran: %d output lines, want %d", got, before)
	}

	// Revoking the directory blocks the next fire too.
	d.asUser(daemonBin + " deny " + daemonProject)
	before, start = d.outputLines(), time.Now()
	d.waitFor("revoked run to be refused", func() bool { return d.blockedRuns() >= 2 })
	t.Logf("revoked fire refused after %s", time.Since(start).Round(time.Second))
	d.requireBlockedReason("unauthorized")
	if got := d.outputLines(); got != before {
		t.Fatalf("revoked entry ran: %d output lines, want %d", got, before)
	}

	// Replacing the binary in place keeps installed entries working.
	d.asUser(daemonBin + " allow " + daemonProject)
	d.install(buildLinuxBinary(t, "0.0.1-daemon"), daemonBin)
	if got := d.asUser(daemonBin + " --version"); got != "0.0.1-daemon\n" {
		t.Fatalf("upgraded binary reports %q", got)
	}
	before, start = d.outputLines(), time.Now()
	d.waitFor("run after an in-place upgrade", func() bool { return d.outputLines() > before })
	t.Logf("first fire after the in-place upgrade after %s", time.Since(start).Round(time.Second))

	// Syncing from a moved binary refreshes every entry to the new path.
	d.asRoot("mkdir -p " + filepath.Dir(movedBin) + " && cp " + daemonBin + " " + movedBin + " && chmod 0755 " + movedBin)
	d.asUser(movedBin + " crontab sync")
	d.asRoot("rm " + daemonBin)
	d.bin = movedBin
	if installed := d.asUser("crontab -l"); !strings.Contains(installed, "'"+movedBin+"'") || strings.Contains(installed, "'"+daemonBin+"'") {
		t.Fatalf("sync from the moved binary left stale entries:\n%s", installed)
	}
	before, start = d.outputLines(), time.Now()
	d.waitFor("run through the moved binary", func() bool { return d.outputLines() > before })
	t.Logf("first fire through the moved binary after %s", time.Since(start).Round(time.Second))

	d.asUser(movedBin + " crontab uninstall")
	if installed := d.asUser("crontab -l"); strings.TrimRight(installed, "\n") != personalJob {
		t.Fatalf("uninstall left %q, want only the personal job", installed)
	}
}

type daemon struct {
	t   *testing.T
	ctx context.Context
	ctr *testcontainers.DockerContainer
	// bin is the cron-person path the installed entries currently use.
	bin string
}

func (d *daemon) asUser(script string) string { return d.run(script, tcexec.WithUser(daemonUser)) }

func (d *daemon) asRoot(script string) string { return d.run(script, tcexec.WithUser("root")) }

func (d *daemon) run(script string, user tcexec.ProcessOption) string {
	d.t.Helper()
	// docker exec keeps the container's root environment, so HOME must name
	// the tester's home for the default store, and AGENT=1 keeps output parseable.
	env := tcexec.WithEnv([]string{"HOME=/home/" + daemonUser, "AGENT=1"})
	code, out, err := d.ctr.Exec(d.ctx, []string{"sh", "-c", script}, user, env, tcexec.Multiplexed())
	if err != nil {
		d.t.Fatalf("exec %q: %v", script, err)
	}
	data, err := io.ReadAll(out)
	if err != nil {
		d.t.Fatal(err)
	}
	if code != 0 {
		d.t.Fatalf("exec %q exited %d: %s", script, code, data)
	}
	return string(data)
}

func (d *daemon) install(hostPath, containerPath string) {
	d.t.Helper()
	staged := containerPath + ".new"
	if err := d.ctr.CopyFileToContainer(d.ctx, hostPath, staged, 0o755); err != nil {
		d.t.Fatal(err)
	}
	// Renaming over the old file is how installers replace a running binary.
	d.asRoot("mv " + staged + " " + containerPath)
}

func (d *daemon) outputLines() int {
	d.t.Helper()
	n, err := strconv.Atoi(strings.TrimSpace(d.asUser("touch " + daemonOutput + " && wc -l < " + daemonOutput)))
	if err != nil {
		d.t.Fatal(err)
	}
	return n
}

// history returns the scheduled runs, newest first.
func (d *daemon) history() []string {
	d.t.Helper()
	return strings.Split(strings.TrimRight(d.asUser(d.bin+" task history --limit 0"), "\n"), "\n")
}

func (d *daemon) blockedRuns() int {
	return strings.Count(strings.Join(d.history(), "\n"), "status: blocked")
}

func (d *daemon) requireBlockedReason(reason string) {
	d.t.Helper()
	newest := d.history()[0]
	id, _, _ := strings.Cut(strings.TrimPrefix(newest, "id: "), "\t")
	if log := d.asUser(d.bin + " task log " + id); !strings.Contains(log, reason) {
		d.t.Fatalf("blocked run %s does not mention %q:\n%s", id, reason, log)
	}
}

func (d *daemon) waitFor(what string, done func() bool) {
	d.t.Helper()
	for deadline := time.Now().Add(fireTimeout); time.Now().Before(deadline); time.Sleep(2 * time.Second) {
		if done() {
			return
		}
	}
	d.t.Fatalf("timed out waiting for %s; crontab:\n%s", what, d.asUser("crontab -l"))
}

// buildLinuxBinary cross-compiles cron-person for the container's Linux
// architecture, which matches the host on Docker Desktop and Linux runners.
func buildLinuxBinary(t *testing.T, version string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "cron-person")
	build := exec.Command("go", "build", "-ldflags", "-X main.version="+version, "-o", out, ".")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, data)
	}
	return out
}
