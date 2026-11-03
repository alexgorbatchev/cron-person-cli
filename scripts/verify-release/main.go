// Command verify-release checks a published GitHub release of cron-person
// without changing anything: the hosted workflow verified before releasing,
// the assets match .goreleaser.yml and checksums.txt, and every binary
// carries the release version.
package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	repo     = "alexgorbatchev/cron-person-cli"
	workflow = "release.yml"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: verify-release <tag>")
		return 2
	}
	v := &verifier{
		api:    "https://api.github.com",
		client: &http.Client{Timeout: 5 * time.Minute},
		token:  os.Getenv("GITHUB_TOKEN"),
		config: ".goreleaser.yml",
		out:    stdout,
	}
	if err := v.verify(args[0]); err != nil {
		fmt.Fprintln(stderr, "FAIL:", err)
		return 1
	}
	return 0
}

type verifier struct {
	api    string
	client *http.Client
	token  string
	config string
	out    io.Writer
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type goreleaserConfig struct {
	ProjectName string `yaml:"project_name"`
	Builds      []struct {
		Binary string   `yaml:"binary"`
		Goos   []string `yaml:"goos"`
		Goarch []string `yaml:"goarch"`
	} `yaml:"builds"`
}

// verify reports every check it passes and returns all failures together, so
// one run shows the whole state of a release.
func (v *verifier) verify(tag string) error {
	cfg, err := loadConfig(v.config)
	if err != nil {
		return err
	}
	var failures []error
	fail := func(err error) {
		if err != nil {
			failures = append(failures, err)
		}
	}
	fail(v.checkWorkflow(tag))

	// The by-tag endpoint serves published releases only, so a draft or a
	// missing release both fail here.
	var rel release
	if err := v.getJSON("/repos/"+repo+"/releases/tags/"+url.PathEscape(tag), &rel); err != nil {
		return errors.Join(append(failures, err)...)
	}
	dir, err := os.MkdirTemp("", "verify-release-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir) // Downloads are scratch.
	fail(v.checkAssets(cfg, strings.TrimPrefix(tag, "v"), rel.Assets, dir))
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	v.report("release %s verified", tag)
	return nil
}

func loadConfig(path string) (goreleaserConfig, error) {
	var cfg goreleaserConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}
	if cfg.ProjectName == "" || len(cfg.Builds) != 1 {
		return cfg, fmt.Errorf("%s must name the project and declare exactly one build", path)
	}
	return cfg, nil
}

type workflowRun struct {
	ID         int64  `json:"id"`
	HeadBranch string `json:"head_branch"`
	Event      string `json:"event"`
	Conclusion string `json:"conclusion"`
}

// checkWorkflow requires the newest release run for tag to have succeeded,
// with the publishing job starting only after verification passed.
func (v *verifier) checkWorkflow(tag string) error {
	var runs struct {
		Runs []workflowRun `json:"workflow_runs"`
	}
	if err := v.getJSON("/repos/"+repo+"/actions/workflows/"+workflow+"/runs?"+url.Values{"branch": {tag}}.Encode(), &runs); err != nil {
		return err
	}
	i := slices.IndexFunc(runs.Runs, func(r workflowRun) bool {
		return r.HeadBranch == tag && r.Event == "push"
	})
	if i < 0 {
		return fmt.Errorf("no %s run for tag %s", workflow, tag)
	}
	run := runs.Runs[i]
	if run.Conclusion != "success" {
		return fmt.Errorf("%s run %d concluded %q", workflow, run.ID, run.Conclusion)
	}

	var jobs struct {
		Jobs []struct {
			Name        string    `json:"name"`
			Conclusion  string    `json:"conclusion"`
			StartedAt   time.Time `json:"started_at"`
			CompletedAt time.Time `json:"completed_at"`
		} `json:"jobs"`
	}
	if err := v.getJSON(fmt.Sprintf("/repos/%s/actions/runs/%d/jobs", repo, run.ID), &jobs); err != nil {
		return err
	}
	byName := map[string]int{}
	for i, job := range jobs.Jobs {
		byName[job.Name] = i
	}
	verifyIdx, okVerify := byName["verify"]
	releaseIdx, okRelease := byName["release"]
	if !okVerify || !okRelease {
		return fmt.Errorf("run %d lacks a verify or release job", run.ID)
	}
	verifyJob, releaseJob := jobs.Jobs[verifyIdx], jobs.Jobs[releaseIdx]
	if verifyJob.Conclusion != "success" || releaseJob.Conclusion != "success" {
		return fmt.Errorf("run %d: verify %q, release %q", run.ID, verifyJob.Conclusion, releaseJob.Conclusion)
	}
	if releaseJob.StartedAt.Before(verifyJob.CompletedAt) {
		return fmt.Errorf("run %d: release started %s, before verify completed %s", run.ID, releaseJob.StartedAt, verifyJob.CompletedAt)
	}
	v.report("workflow run %d: verify succeeded %s-%s, then release succeeded %s-%s", run.ID,
		verifyJob.StartedAt.Format(time.RFC3339), verifyJob.CompletedAt.Format(time.RFC3339),
		releaseJob.StartedAt.Format(time.RFC3339), releaseJob.CompletedAt.Format(time.RFC3339))
	return nil
}

// checkAssets downloads every asset into dir and checks the inventory,
// checksums, and the binary inside every archive.
func (v *verifier) checkAssets(cfg goreleaserConfig, version string, assets []asset, dir string) error {
	build := cfg.Builds[0]
	type target struct{ goos, goarch string }
	archives := map[string]target{}
	for _, goos := range build.Goos {
		ext := ".tar.gz"
		if goos == "windows" {
			ext = ".zip"
		}
		for _, goarch := range build.Goarch {
			t := target{goos, goarch}
			archives[fmt.Sprintf("%s_%s_%s_%s%s", cfg.ProjectName, version, goos, goarch, ext)] = t
			archives[fmt.Sprintf("%s_%s_%s%s", cfg.ProjectName, goos, goarch, ext)] = t
		}
	}

	var failures []error
	published := map[string]string{}
	for _, a := range assets {
		published[a.Name] = filepath.Join(dir, a.Name)
		if _, ok := archives[a.Name]; !ok && a.Name != "checksums.txt" {
			failures = append(failures, fmt.Errorf("unexpected asset %s", a.Name))
		}
	}
	for name := range archives {
		if _, ok := published[name]; !ok {
			failures = append(failures, fmt.Errorf("missing asset %s", name))
		}
	}
	if _, ok := published["checksums.txt"]; !ok {
		return errors.Join(append(failures, errors.New("missing asset checksums.txt"))...)
	}
	if len(failures) == 0 {
		v.report("inventory: %d assets for %d targets", len(assets), len(archives)/2)
	}

	for _, a := range assets {
		if err := v.download(a.URL, published[a.Name]); err != nil {
			return errors.Join(append(failures, err)...)
		}
	}
	sums, err := readChecksums(published["checksums.txt"])
	if err != nil {
		return errors.Join(append(failures, err)...)
	}
	mismatched := len(failures)
	for name, path := range published {
		if name == "checksums.txt" {
			continue
		}
		want, ok := sums[name]
		if !ok {
			failures = append(failures, fmt.Errorf("%s is not in checksums.txt", name))
			continue
		}
		if got, err := fileSHA256(path); err != nil || got != want {
			failures = append(failures, fmt.Errorf("%s: sha256 %s, checksums.txt says %s (%v)", name, got, want, err))
		}
	}
	if len(failures) == mismatched {
		v.report("checksums: %d archives match checksums.txt", len(sums))
	}

	checked := len(failures)
	for name, t := range archives {
		path, ok := published[name]
		if !ok {
			continue
		}
		binary := build.Binary
		if t.goos == "windows" {
			binary += ".exe"
		}
		extracted := filepath.Join(dir, "extracted", name, binary)
		if err := extract(path, binary, extracted); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
			continue
		}
		if err := checkBinary(extracted, version, t.goos, t.goarch); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
			continue
		}
		if t.goos == runtime.GOOS && t.goarch == runtime.GOARCH {
			if err := checkRunningVersion(extracted, version); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", name, err))
				continue
			}
		}
	}
	if len(failures) == checked {
		v.report("binaries: every archive holds %s built for its target with version %s", build.Binary, version)
	}
	return errors.Join(failures...)
}

func readChecksums(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sums := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		sum, name, ok := strings.Cut(scanner.Text(), "  ")
		if !ok {
			return nil, fmt.Errorf("malformed checksums.txt line %q", scanner.Text())
		}
		sums[name] = sum
	}
	return sums, scanner.Err()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extract copies the archive member named binary to dest.
func extract(archive, binary, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.Name == binary {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				defer rc.Close()
				return writeExecutable(dest, rc)
			}
		}
		return fmt.Errorf("archive lacks %s", binary)
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("archive lacks %s", binary)
		}
		if err != nil {
			return err
		}
		if hdr.Name == binary {
			return writeExecutable(dest, tr)
		}
	}
}

func writeExecutable(dest string, r io.Reader) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	return errors.Join(copyErr, f.Close())
}

// checkBinary reads the build information Go embeds in every binary, so
// foreign-platform binaries are checked without being executed.
func checkBinary(path, version, goos, goarch string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading build info: %w", err)
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	if settings["GOOS"] != goos || settings["GOARCH"] != goarch {
		return fmt.Errorf("built for %s/%s", settings["GOOS"], settings["GOARCH"])
	}
	if got := injectedVersion(settings["-ldflags"]); got != version {
		return fmt.Errorf("ldflags %q set version %q, want %s", settings["-ldflags"], got, version)
	}
	return nil
}

// injectedVersion returns the value the linker gives main.version: the last
// -X assignment to it wins.
func injectedVersion(ldflags string) string {
	var version string
	fields := strings.Fields(ldflags)
	for i, field := range fields {
		assignment, ok := strings.CutPrefix(field, "-X=")
		if field == "-X" && i+1 < len(fields) {
			assignment, ok = fields[i+1], true
		}
		if value, found := strings.CutPrefix(assignment, "main.version="); ok && found {
			version = value
		}
	}
	return version
}

func checkRunningVersion(path, version string) error {
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return fmt.Errorf("running --version: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != version {
		return fmt.Errorf("--version printed %q, want %q", got, version)
	}
	return nil
}

func (v *verifier) getJSON(path string, into any) error {
	resp, err := v.get(v.api+path, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}
	return nil
}

func (v *verifier) download(url, dest string) error {
	resp, err := v.get(url, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	return errors.Join(copyErr, f.Close())
}

// get fetches url, sending the token only to the API so it never reaches the
// hosts that serve release downloads.
func (v *verifier) get(url string, api bool) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if v.token != "" && api {
		req.Header.Set("Authorization", "Bearer "+v.token)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func (v *verifier) report(format string, args ...any) {
	fmt.Fprintf(v.out, "ok: "+format+"\n", args...)
}
