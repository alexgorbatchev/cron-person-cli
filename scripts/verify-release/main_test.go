package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const fixtureVersion = "1.2.3"

// binaries holds fakecli builds keyed by "goos/goarch/version".
var binaries = map[string][]byte{}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "verify-release-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	code := func() int {
		defer os.RemoveAll(dir)
		for _, b := range []struct{ goos, version string }{
			{runtime.GOOS, fixtureVersion},
			{"windows", fixtureVersion},
			{runtime.GOOS, "9.9.9"},
		} {
			out := filepath.Join(dir, b.goos+b.version)
			build := exec.Command("go", "build", "-ldflags", "-s -w -X main.version="+b.version, "-o", out, "./testdata/fakecli")
			build.Env = append(os.Environ(), "GOOS="+b.goos, "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
			if data, err := build.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "building fakecli: %v: %s\n", err, data)
				return 2
			}
			data, err := os.ReadFile(out)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			binaries[b.goos+"/"+runtime.GOARCH+"/"+b.version] = data
		}
		return m.Run()
	}()
	os.Exit(code)
}

type job struct {
	Name        string    `json:"name"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// fixture is a published release served by a fake GitHub.
type fixture struct {
	config    string
	draft     bool
	runs      []workflowRun
	jobs      []job
	assets    map[string][]byte
	checksums string
	missing   map[string]bool
	// phantom assets are listed by the release but not served.
	phantom   []string
	brokenAPI bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	config := filepath.Join(t.TempDir(), ".goreleaser.yml")
	content := fmt.Sprintf("project_name: tool\nbuilds:\n  - binary: tool\n    goos: [%s, windows]\n    goarch: [%s]\n", runtime.GOOS, runtime.GOARCH)
	if err := os.WriteFile(config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	verifyDone := time.Date(2026, 11, 1, 20, 52, 29, 0, time.UTC)
	f := &fixture{
		config: config,
		runs:   []workflowRun{{ID: 7, HeadBranch: "v" + fixtureVersion, Event: "push", Conclusion: "success"}},
		jobs: []job{
			{"verify", "success", verifyDone.Add(-20 * time.Second), verifyDone},
			{"release", "success", verifyDone.Add(time.Second), verifyDone.Add(time.Minute)},
		},
		assets:  map[string][]byte{},
		missing: map[string]bool{},
	}
	for _, goos := range []string{runtime.GOOS, "windows"} {
		archive := tarGz(t, "tool", binaries[goos+"/"+runtime.GOARCH+"/"+fixtureVersion])
		ext := ".tar.gz"
		if goos == "windows" {
			archive, ext = zipped(t, "tool.exe", binaries[goos+"/"+runtime.GOARCH+"/"+fixtureVersion]), ".zip"
		}
		f.assets[fmt.Sprintf("tool_%s_%s_%s%s", fixtureVersion, goos, runtime.GOARCH, ext)] = archive
		f.assets[fmt.Sprintf("tool_%s_%s%s", goos, runtime.GOARCH, ext)] = archive
	}
	return f
}

func (f *fixture) verify(t *testing.T) (string, error) {
	t.Helper()
	checksums := f.checksums
	if checksums == "" {
		for name, data := range f.assets {
			sum := sha256.Sum256(data)
			checksums += hex.EncodeToString(sum[:]) + "  " + name + "\n"
		}
	}
	mux := http.NewServeMux()
	var srv *httptest.Server
	serveJSON := func(path string, body func() any) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(body())
		})
	}
	api := "/repos/" + repo
	serveJSON(api+"/actions/workflows/"+workflow+"/runs", func() any { return map[string]any{"workflow_runs": f.runs} })
	serveJSON(api+"/actions/runs/7/jobs", func() any { return map[string]any{"jobs": f.jobs} })
	serveJSON(api+"/releases/tags/v"+fixtureVersion, func() any {
		assets := []asset{{Name: "checksums.txt", URL: srv.URL + "/download/checksums.txt"}}
		for name := range f.assets {
			if !f.missing[name] {
				assets = append(assets, asset{Name: name, URL: srv.URL + "/download/" + name})
			}
		}
		for _, name := range f.phantom {
			assets = append(assets, asset{Name: name, URL: srv.URL + "/download/" + name})
		}
		if f.brokenAPI {
			return "not a release"
		}
		return release{TagName: "v" + fixtureVersion, Draft: f.draft, Assets: assets}
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "token sent to a download host", http.StatusBadRequest)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/download/")
		if name == "checksums.txt" {
			_, _ = w.Write([]byte(checksums))
			return
		}
		data, ok := f.assets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	var out bytes.Buffer
	v := &verifier{api: srv.URL, client: srv.Client(), token: "test-token", config: f.config, out: &out}
	err := v.verify("v" + fixtureVersion)
	return out.String(), err
}

func TestVerifyAcceptsAConsistentRelease(t *testing.T) {
	out, err := newFixture(t).verify(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"workflow run 7: verify succeeded", "inventory: 5 assets for 2 targets", "checksums: 4 archives", "binaries: every archive", "release v" + fixtureVersion + " verified"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestVerifyReportsInconsistentReleases(t *testing.T) {
	native := fmt.Sprintf("tool_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	windows := fmt.Sprintf("tool_windows_%s.zip", runtime.GOARCH)
	for _, tt := range []struct {
		name   string
		break_ func(t *testing.T, f *fixture)
		want   string
	}{
		{"failed run", func(_ *testing.T, f *fixture) { f.runs[0].Conclusion = "failure" }, `concluded "failure"`},
		{"no run", func(_ *testing.T, f *fixture) { f.runs[0].HeadBranch = "main" }, "no release.yml run"},
		{"missing job", func(_ *testing.T, f *fixture) { f.jobs = f.jobs[:1] }, "lacks a verify or release job"},
		{"failed job", func(_ *testing.T, f *fixture) { f.jobs[0].Conclusion = "failure" }, `verify "failure"`},
		{"release before verify", func(_ *testing.T, f *fixture) { f.jobs[1].StartedAt = f.jobs[0].StartedAt }, "before verify completed"},
		{"draft", func(_ *testing.T, f *fixture) { f.draft = true }, "is a draft"},
		{"missing asset", func(_ *testing.T, f *fixture) { f.missing[native] = true }, "missing asset " + native},
		{"unexpected asset", func(_ *testing.T, f *fixture) { f.assets["extra.txt"] = []byte("x") }, "unexpected asset extra.txt"},
		{"checksum mismatch", func(_ *testing.T, f *fixture) {
			f.checksums = strings.Repeat("0", 64) + "  " + native + "\n"
		}, native + ": sha256"},
		{"unlisted archive", func(_ *testing.T, f *fixture) { f.checksums = "abc  other\n" }, "is not in checksums.txt"},
		{"malformed checksums", func(_ *testing.T, f *fixture) { f.checksums = "garbage\n" }, "malformed checksums.txt"},
		{"wrong version", func(t *testing.T, f *fixture) {
			f.assets[native] = tarGz(t, "tool", binaries[runtime.GOOS+"/"+runtime.GOARCH+"/9.9.9"])
		}, "do not set version " + fixtureVersion},
		{"wrong platform", func(t *testing.T, f *fixture) {
			f.assets[native] = tarGz(t, "tool", binaries["windows/"+runtime.GOARCH+"/"+fixtureVersion])
		}, "built for windows/"},
		{"missing binary", func(t *testing.T, f *fixture) { f.assets[native] = tarGz(t, "README", []byte("x")) }, "archive lacks tool"},
		{"not a binary", func(t *testing.T, f *fixture) { f.assets[native] = tarGz(t, "tool", []byte("x")) }, "reading build info"},
		{"corrupt archive", func(_ *testing.T, f *fixture) { f.assets[native] = []byte("not gzip") }, native},
		{"zip without binary", func(t *testing.T, f *fixture) { f.assets[windows] = zipped(t, "README", []byte("x")) }, "archive lacks tool.exe"},
		{"corrupt zip", func(_ *testing.T, f *fixture) { f.assets[windows] = []byte("not zip") }, windows},
		{"unserved asset", func(_ *testing.T, f *fixture) { f.phantom = []string{"gone.zip"} }, "404"},
		{"undecodable release", func(_ *testing.T, f *fixture) { f.brokenAPI = true }, "decoding"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.break_(t, f)
			if _, err := f.verify(t); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestVerifyReportsUnusableInputs(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(f.config, []byte("project_name: tool\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.verify(t); err == nil || !strings.Contains(err.Error(), "exactly one build") {
		t.Fatalf("incomplete config error = %v", err)
	}
	if err := os.WriteFile(f.config, []byte("builds: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.verify(t); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("invalid config error = %v", err)
	}
	f.config = filepath.Join(t.TempDir(), "missing.yml")
	if _, err := f.verify(t); err == nil || !strings.Contains(err.Error(), "reading") {
		t.Fatalf("missing config error = %v", err)
	}

	v := &verifier{api: "http://127.0.0.1:1", client: http.DefaultClient, config: newFixture(t).config, out: &bytes.Buffer{}}
	if err := v.verify("v" + fixtureVersion); err == nil {
		t.Fatal("unreachable API accepted")
	}
}

func TestRunReportsUsageAndFailures(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage: verify-release <tag>") {
		t.Fatalf("no arguments: exit %d, stderr %q", code, stderr.String())
	}
	stderr.Reset()
	// The default configuration path is relative, so outside the repository
	// root the run fails before reaching the network.
	t.Chdir(t.TempDir())
	if code := run([]string{"v" + fixtureVersion}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "FAIL: reading .goreleaser.yml") {
		t.Fatalf("missing configuration: exit %d, stderr %q", code, stderr.String())
	}
}

func TestCheckRunningVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, binaries[runtime.GOOS+"/"+runtime.GOARCH+"/9.9.9"], 0o700); err != nil {
		t.Fatal(err)
	}
	if err := checkRunningVersion(path, fixtureVersion); err == nil || !strings.Contains(err.Error(), `printed "9.9.9"`) {
		t.Fatalf("mismatched version error = %v", err)
	}
	if err := checkRunningVersion(filepath.Join(t.TempDir(), "missing"), fixtureVersion); err == nil {
		t.Fatal("missing binary accepted")
	}
}

func tarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipped(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
