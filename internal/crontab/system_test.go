package crontab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func isolatedCrontab(t *testing.T) string {
	t.Helper()
	fixture, err := filepath.Abs("../../testdata")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "crontab")
	t.Setenv("PATH", fixture)
	t.Setenv("TEST_CRONTAB_FILE", path)
	t.Setenv("TEST_CRONTAB_READ_ERROR", "")
	t.Setenv("TEST_CRONTAB_WRITE_ERROR", "")
	t.Setenv("TEST_CRONTAB_READ_BARRIER", "")
	return path
}

func writeCrontabFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSystemCrontabRoundTrip(t *testing.T) {
	path := isolatedCrontab(t)
	if got, err := ReadSystemCrontab(); err != nil || got != "" {
		t.Fatalf("missing crontab=%q error=%v", got, err)
	}
	want := "MAILTO=ops@example.com\n0 3 * * * backup\n"
	if err := WriteSystemCrontab(want); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadSystemCrontab(); err != nil || got != want {
		t.Fatalf("round trip=%q error=%v, want %q", got, err, want)
	}
	calls, err := os.ReadFile(path + ".calls")
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "-l\n-\n-l\n" {
		t.Fatalf("unexpected subprocess arguments: %q", calls)
	}
}

func TestSystemCrontabFailures(t *testing.T) {
	for _, tt := range []struct {
		name, readError, writeError, want string
		missing                           bool
	}{
		{"read failure", "read refused", "", "reading system crontab", false},
		{"write failure", "", "write refused", "installing crontab", false},
		{"missing executable", "", "", "executable file not found", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := isolatedCrontab(t)
			writeCrontabFile(t, path, "@daily keep-me\n")
			t.Setenv("TEST_CRONTAB_READ_ERROR", tt.readError)
			t.Setenv("TEST_CRONTAB_WRITE_ERROR", tt.writeError)
			if tt.missing {
				t.Setenv("PATH", t.TempDir())
			}
			var err error
			if tt.writeError != "" {
				err = WriteSystemCrontab("replacement\n")
			} else {
				_, err = ReadSystemCrontab()
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "@daily keep-me\n" {
				t.Fatalf("failed operation changed crontab: %q, %v", data, err)
			}
		})
	}
}

func TestSyncAndUninstall(t *testing.T) {
	path := isolatedCrontab(t)
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"active", "changed", "missing"} {
		project := filepath.Join(dir, name)
		if err := os.Mkdir(project, 0o700); err != nil {
			t.Fatal(err)
		}
		cronrc := filepath.Join(project, ".cronrc")
		writeCrontabFile(t, cronrc, "@daily run-"+name+"\n")
		if _, err := st.Allow(project); err != nil {
			t.Fatal(err)
		}
		switch name {
		case "changed":
			writeCrontabFile(t, cronrc, "@hourly changed-command\n")
		case "missing":
			if err := os.Remove(cronrc); err != nil {
				t.Fatal(err)
			}
		}
	}
	userJobs := "MAILTO=ops@example.com\n@weekly personal-backup\n"
	writeCrontabFile(t, path, userJobs+"\n"+BlockStart+"\n@daily stale-job\n"+BlockEnd+"\n")
	var first string
	for i := range 2 {
		count, err := Sync(st, "/opt/cron-person", testConfig)
		if err != nil || count != 1 {
			t.Fatalf("synced directories=%d error=%v", count, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got := string(data)
		if !strings.HasPrefix(got, userJobs) || !strings.Contains(got, "run-active") ||
			strings.Contains(got, "stale-job") || strings.Contains(got, "changed-command") ||
			strings.Contains(got, "run-missing") || strings.Count(got, BlockStart) != 1 {
			t.Fatalf("unexpected synchronized crontab: %s", got)
		}
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatalf("sync is not idempotent: first=%q second=%q", first, got)
		}
	}
	if err := Uninstall(filepath.Dir(st.Path())); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), userJobs) || strings.Contains(string(data), BlockStart) {
		t.Fatalf("uninstall lost user jobs or kept managed jobs: %q, %v", data, err)
	}
}

func TestSyncAndUninstallFailures(t *testing.T) {
	for _, operation := range []string{"sync", "uninstall"} {
		for _, failure := range []string{"read", "write"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				path := isolatedCrontab(t)
				writeCrontabFile(t, path, "@daily keep-me\n")
				st, err := store.NewStore(filepath.Join(t.TempDir(), "state.json"))
				if err != nil {
					t.Fatal(err)
				}
				if failure == "read" {
					t.Setenv("TEST_CRONTAB_READ_ERROR", "read refused")
				} else {
					t.Setenv("TEST_CRONTAB_WRITE_ERROR", "write refused")
				}
				if operation == "sync" {
					_, err = Sync(st, "cron-person", testConfig)
				} else {
					err = Uninstall(filepath.Dir(st.Path()))
				}
				if err == nil || !strings.Contains(err.Error(), failure+" refused") {
					t.Fatalf("lost subprocess error: %v", err)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "@daily keep-me\n" {
					t.Fatalf("failure modified user crontab: %q, %v", data, err)
				}
			})
		}
	}
}

func TestGenerateBlockSkipsUnusableFiles(t *testing.T) {
	dir := t.TempDir()
	records := make([]store.Record, 0, 4)
	for _, tt := range []struct{ name, content string }{
		{"empty", "ENV=only\n"},
		{"valid", "@hourly run-active\n"},
	} {
		path := filepath.Join(dir, tt.name)
		writeCrontabFile(t, path, tt.content)
		records = append(records, store.Record{Dir: dir, Path: path})
	}
	records = append(records, store.Record{Dir: dir, Path: filepath.Join(dir, "missing")})
	state := filepath.Join(dir, "state.json")
	block, err := GenerateBlock(records, Invocation{Bin: "", Store: state, Config: testConfig})
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "@hourly '"+exe+"' --store '"+state+"' --config '"+testConfig+"' exec '"+dir+"' -- 'run-active'") || strings.Count(block, "# Directory:") != 1 {
		t.Fatalf("unexpected generated block: %s", block)
	}
}

func TestGenerateBlockReportsConfigFailures(t *testing.T) {
	for _, failure := range []string{"reading", "parsing"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".cronrc")
			if failure == "reading" {
				if err := os.Symlink(".cronrc", path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeCrontabFile(t, path, "invalid schedule\n")
			}
			block, err := GenerateBlock([]store.Record{{Dir: dir, Path: path}}, Invocation{Bin: "/opt/cron-person", Store: filepath.Join(dir, "state.json"), Config: testConfig})
			if err == nil || !strings.Contains(err.Error(), failure+" "+path) || block != "" {
				t.Fatalf("block=%q error=%v", block, err)
			}
		})
	}
}

func TestMergeCrontabEmptyInputs(t *testing.T) {
	for _, tt := range []struct{ name, existing, managed, want string }{
		{"empty", "", "", ""},
		{"remove only block", BlockStart + "\nold\n" + BlockEnd + "\n", "", ""},
		{"keep unmanaged", "user job\n\n", "", "user job\n"},
		{"new only block", "", "managed", "managed\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeCrontab(tt.existing, tt.managed)
			if err != nil || got != tt.want {
				t.Fatalf("merged=%q err=%v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestSyncAndUninstallPreserveMarkerSubstringsAndRejectMalformed(t *testing.T) {
	path := isolatedCrontab(t)
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(dir, "app")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeCrontabFile(t, filepath.Join(project, ".cronrc"), "@daily run-app\n")
	if _, err := st.Allow(project); err != nil {
		t.Fatal(err)
	}

	userJobs := "@daily echo '" + BlockStart + "'\n@hourly personal-job\n@daily echo '" + BlockEnd + "'\n"
	writeCrontabFile(t, path, userJobs)

	if err := Uninstall(filepath.Dir(st.Path())); err != nil {
		t.Fatalf("Uninstall without managed block err=%v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != userJobs {
		t.Fatalf("Uninstall modified unmanaged marker substrings: got=%q want=%q err=%v", data, userJobs, err)
	}

	if count, err := Sync(st, "/opt/cron-person", testConfig); err != nil || count != 1 {
		t.Fatalf("Sync count=%d err=%v", count, err)
	}
	syncedData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(syncedData), userJobs) || !strings.Contains(string(syncedData), "run-app") {
		t.Fatalf("Sync failed to preserve unmanaged marker substrings: %q", syncedData)
	}

	if err := Uninstall(filepath.Dir(st.Path())); err != nil {
		t.Fatalf("Uninstall err=%v", err)
	}
	uninstalledData, err := os.ReadFile(path)
	if err != nil || strings.TrimRight(string(uninstalledData), "\n")+"\n" != userJobs || strings.Contains(string(uninstalledData), "run-app") {
		t.Fatalf("Uninstall modified unmanaged marker substrings: got=%q want=%q err=%v", uninstalledData, userJobs, err)
	}

	for _, malformed := range []string{
		userJobs + BlockStart + "\n@hourly unclosed\n",
		userJobs + BlockEnd + "\n",
		BlockStart + "\n@hourly one\n" + BlockEnd + "\n@daily middle\n" + BlockStart + "\n@hourly two\n" + BlockEnd + "\n",
	} {
		writeCrontabFile(t, path, malformed)
		if _, err := Sync(st, "/opt/cron-person", testConfig); err == nil {
			t.Fatalf("expected Sync to reject malformed crontab %q", malformed)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != malformed {
			t.Fatalf("Sync modified malformed crontab: got=%q want=%q err=%v", data, malformed, err)
		}
		if err := Uninstall(filepath.Dir(st.Path())); err == nil {
			t.Fatalf("expected Uninstall to reject malformed crontab %q", malformed)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != malformed {
			t.Fatalf("Uninstall modified malformed crontab: got=%q want=%q err=%v", data, malformed, err)
		}
	}
}

// pausedUpdate prepares an isolated crontab holding userJobs and a store with
// one allowed project, and arms the fixture to pause every read after it has
// produced the crontab.
func pausedUpdate(t *testing.T, userJobs string) (crontabPath string, st *store.Store, release func(), reached string) {
	t.Helper()
	crontabPath = isolatedCrontab(t)
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(dir, "state", "allowed.json"))
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(dir, "app")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeCrontabFile(t, filepath.Join(project, ".cronrc"), "@daily run-app\n")
	if _, err := st.Allow(project); err != nil {
		t.Fatal(err)
	}
	writeCrontabFile(t, crontabPath, userJobs)
	barrier := filepath.Join(dir, "barrier")
	t.Setenv("TEST_CRONTAB_READ_BARRIER", barrier)
	release = func() {
		if err := os.WriteFile(barrier+".release", nil, 0o600); err != nil {
			t.Error(err)
		}
	}
	// A failed test must not leave fixture reads waiting forever.
	t.Cleanup(release)
	return crontabPath, st, release, barrier + ".reached"
}

func TestConcurrentUpdatesWaitForTheUpdateInFlight(t *testing.T) {
	userJobs := "@weekly personal-backup\n"
	path, st, release, reached := pausedUpdate(t, userJobs)

	syncDone := make(chan error, 1)
	go func() {
		_, err := Sync(st, "/opt/cron-person", testConfig)
		syncDone <- err
	}()
	waitForPath(t, reached)

	// A second cron-person update starts while the sync sits between reading
	// and installing the crontab; it must not read until the sync installs.
	uninstallDone := make(chan error, 1)
	go func() { uninstallDone <- Uninstall(filepath.Dir(st.Path())) }()
	time.Sleep(200 * time.Millisecond)
	if calls := crontabCalls(t, path); calls != "-l\n" {
		t.Fatalf("uninstall ran during the sync's update: calls=%q", calls)
	}

	release()
	for _, done := range []chan error{syncDone, uninstallDone} {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls := crontabCalls(t, path); calls != "-l\n-\n-l\n-\n" {
		t.Fatalf("updates interleaved: calls=%q", calls)
	}
	if data, err := os.ReadFile(path); err != nil || strings.TrimRight(string(data), "\n")+"\n" != userJobs {
		t.Fatalf("crontab=%q err=%v, want the personal jobs without the managed block", data, err)
	}
}

// TestUpdatesOverwriteExternalEditsMadeInFlight pins the documented limit:
// crontab(1) has no compare-and-swap, so a crontab another program installs
// between an update's read and its install is replaced.
func TestUpdatesOverwriteExternalEditsMadeInFlight(t *testing.T) {
	for _, operation := range []string{"sync", "uninstall"} {
		t.Run(operation, func(t *testing.T) {
			userJobs := "@weekly personal-backup\n"
			path, st, release, reached := pausedUpdate(t, userJobs)
			done := make(chan error, 1)
			go func() {
				if operation == "sync" {
					_, err := Sync(st, "/opt/cron-person", testConfig)
					done <- err
					return
				}
				done <- Uninstall(filepath.Dir(st.Path()))
			}()
			waitForPath(t, reached)
			writeCrontabFile(t, path, userJobs+"@daily edited-in-flight\n")
			release()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || strings.Contains(string(data), "edited-in-flight") || !strings.HasPrefix(string(data), userJobs) {
				t.Fatalf("crontab=%q err=%v, want the update's own result", data, err)
			}
		})
	}
}

func TestUpdatesReportUnavailableLock(t *testing.T) {
	isolatedCrontab(t)
	stateDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(stateDir, "crontab.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.NewStore(filepath.Join(stateDir, "allowed.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(st, "/opt/cron-person", testConfig); err == nil || !strings.Contains(err.Error(), "locking crontab updates") {
		t.Fatalf("sync error=%v", err)
	}
	if err := Uninstall(stateDir); err == nil || !strings.Contains(err.Error(), "locking crontab updates") {
		t.Fatalf("uninstall error=%v", err)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	writeCrontabFile(t, blocked, "")
	if err := Uninstall(filepath.Join(blocked, "state")); err == nil || !strings.Contains(err.Error(), "locking crontab updates") {
		t.Fatalf("uninstall with unusable state directory error=%v", err)
	}
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	t.Fatalf("%s never appeared", path)
}

func crontabCalls(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path + ".calls")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
