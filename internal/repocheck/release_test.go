package repocheck

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The release archives hold every binary and the plugin directory, under the
// name the install scripts download (README "Install the plugin", "CI").
func TestReleaseArchivesHoldEverything(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Builds []struct {
			Main, Binary string
			Ldflags      []string
		}
		Archives []struct {
			Formats      []string
			NameTemplate string `yaml:"name_template"`
			IDs          []string
			Files        []any
		}
	}
	if err := yaml.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"./cmd/reliab": "reliab"}
	adapters, err := os.ReadDir(filepath.Join(root, "cmd", "adapters"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range adapters {
		want["./cmd/adapters/"+a.Name()] = "reliab-adapter-" + a.Name()
	}
	have := map[string]string{}
	for _, b := range g.Builds {
		have[b.Main] = b.Binary
		// The binary reports the version the archive is named for, which
		// profile init pins and the installers take.
		if b.Main == "./cmd/reliab" && !strings.Contains(strings.Join(b.Ldflags, " "), "internal/version.Version=v{{ .Version }}") {
			t.Errorf("reliab ldflags %q do not stamp v{{ .Version }}", b.Ldflags)
		}
	}
	for main, bin := range want {
		if have[main] != bin {
			t.Errorf("no build of %s as %s; builds = %v", main, bin, have)
		}
	}
	if len(g.Archives) != 1 {
		t.Fatalf("archives = %+v, want one", g.Archives)
	}
	a := g.Archives[0]
	if len(a.IDs) != 0 || strings.Join(a.Formats, ",") != "tar.gz" || a.NameTemplate != "reliab_{{ .Version }}_{{ .Os }}_{{ .Arch }}" {
		t.Errorf("archive = %+v, want every build as reliab_<version>_<os>_<arch>.tar.gz", a)
	}
	if !strings.Contains(string(raw), "src: plugin/**/*") {
		t.Error("the archive does not include the plugin directory")
	}
}

// The README's install lines name the released version, the one in
// plugin.json, so a reader installs the binary the plugin launcher would
// (README "Install", "Release"; knowledge/decisions/own-ci-checks.md).
func TestReadmeInstallsPluginVersion(t *testing.T) {
	errs, logs := readmeInstallProblems(root)
	for _, l := range logs {
		t.Log(l)
	}
	for _, e := range errs {
		t.Error(e)
	}
}

// readmeInstallProblems returns what is wrong with dir's README install lines,
// and notes on what it allowed.
func readmeInstallProblems(dir string) (errs, logs []string) {
	var p struct{ Version string }
	raw, err := os.ReadFile(filepath.Join(dir, "plugin/.claude-plugin/plugin.json"))
	if err == nil {
		err = json.Unmarshal(raw, &p)
	}
	if err != nil {
		return []string{err.Error()}, nil
	}
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		return []string{err.Error()}, nil
	}
	lines := map[string]*regexp.Regexp{
		"install.sh":  regexp.MustCompile(`scripts/install\.sh \| sh -s -- (\S+)`),
		"install.ps1": regexp.MustCompile(`scripts/install\.ps1\)\)\) -Version (\S+)`),
	}
	var previous string
	var previousErr error
	looked := false
	for name, re := range lines {
		m := re.FindAllStringSubmatch(string(readme), -1)
		if len(m) == 0 {
			errs = append(errs, fmt.Sprintf("README has no %s install line", name))
			continue
		}
		for _, v := range m {
			if v[1] == p.Version {
				continue
			}
			if !looked {
				previous, previousErr = previousPluginVersion(dir)
				looked = true
			}
			switch {
			case previousErr != nil:
				logs = append(logs, fmt.Sprintf("cannot tell plugin.json's previous version (%v); allowing README %s %s", previousErr, name, v[1]))
			case v[1] == previous:
				logs = append(logs, fmt.Sprintf("README %s installs %s while plugin.json moves to %s", name, v[1], p.Version))
			default:
				errs = append(errs, fmt.Sprintf("README %s installs %s, plugin.json is %s (and was %s)", name, v[1], p.Version, previous))
			}
		}
	}
	return errs, logs
}

// previousPluginVersion returns plugin.json's version before the last commit
// that changed it. Only that commit's history decides it, so the answer holds
// across commits that leave plugin.json alone.
func previousPluginVersion(dir string) (string, error) {
	const path = "plugin/.claude-plugin/plugin.json"
	last := exec.Command("git", "log", "-n1", "--format=%H", "--", path)
	last.Dir = dir
	sha, err := last.Output()
	if err != nil || len(bytes.TrimSpace(sha)) == 0 {
		return "", fmt.Errorf("no commit changes %s: %v", path, err)
	}
	show := exec.Command("git", "show", strings.TrimSpace(string(sha))+"^:"+path)
	show.Dir = dir
	raw, err := show.Output()
	if err != nil {
		return "", fmt.Errorf("%s before %s: %v", path, strings.TrimSpace(string(sha)), err)
	}
	var p struct{ Version string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", err
	}
	return p.Version, nil
}

// The README may name the previous version from the version bump until the
// release is out and the README follows (README "Release", the release order),
// however many commits land between them, and no later: a commit that does
// not touch plugin.json changes neither the answer nor what go test's cache
// keys it on (reviewer finding on own-ci-checks.md).
func TestReadmeInstallsPreviousVersionWhileReleasing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	readmeAt := func(v string) {
		t.Helper()
		write("README.md", "curl -fsSL https://x/main/scripts/install.sh | sh -s -- "+v+"\n"+
			"& ([scriptblock]::Create((irm https://x/main/scripts/install.ps1))) -Version "+v+"\n")
	}
	release := func(plugin, readme string) {
		t.Helper()
		write("plugin/.claude-plugin/plugin.json", `{"version":"`+plugin+`"}`)
		readmeAt(readme)
		git("add", "-A")
		git("commit", "-q", "-m", "release "+plugin)
	}
	check := func(want bool) {
		t.Helper()
		errs, _ := readmeInstallProblems(dir)
		if (len(errs) == 0) != want {
			t.Fatalf("passes = %v, want %v; errs = %v", len(errs) == 0, want, errs)
		}
	}
	git("init", "-q")
	release("0.1.4", "0.1.4")
	release("0.1.5", "0.1.5")
	check(true)
	release("0.1.6", "0.1.5") // the bump; the README follows the release
	check(true)
	write("notes.md", "a fix while the release builds")
	git("add", "-A")
	git("commit", "-q", "-m", "unrelated")
	check(true)
	readmeAt("0.1.4")
	check(false) // two versions behind
}

// writeRelease writes a release asset for this machine holding a fresh
// reliab build, and checksums.txt, into a new directory, and returns it.
func writeRelease(t *testing.T, version string, corrupt bool) (dir, asset string) {
	t.Helper()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	bin := filepath.Join(t.TempDir(), "reliab"+ext)
	if b, err := exec.Command("go", "build", "-o", bin, "../../cmd/reliab").CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	body, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	asset = "reliab_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	f, err := os.Create(filepath.Join(dir, asset))
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, content := range map[string][]byte{"reliab" + ext: body, "plugin/.claude-plugin/plugin.json": []byte(`{"name":"reliab"}`)} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []interface{ Close() error }{tw, gz, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, asset))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if corrupt {
		sum[0]++
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(hex.EncodeToString(sum[:])+"  "+asset+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, asset
}

// A released plugin's launcher installs its version from RELIAB_DOWNLOAD_URL,
// which may be a local directory (scripts/install.sh), and refuses an asset
// whose checksum does not match.
func TestLauncherInstallsFromLocalMirror(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	launch := func(mirror, cache string) (string, error) {
		cmd := exec.Command(sh, filepath.Join(root, "plugin/bin/reliab"), "--version")
		env := []string{"RELIAB_VERSION=9.8.7", "RELIAB_DOWNLOAD_URL=" + mirror, "RELIAB_CACHE=" + cache, "GH_TOKEN=", "GITHUB_TOKEN="}
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "RELIAB_") {
				env = append(env, kv)
			}
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	dir, _ := writeRelease(t, "9.8.7", false)
	cache := t.TempDir()
	if out, err := launch(dir, cache); err != nil || !strings.Contains(out, "installed reliab v9.8.7") || !strings.Contains(out, "reliab version") {
		t.Fatalf("install from %s: err=%v out=%s", dir, err, out)
	}
	if _, err := os.Stat(filepath.Join(cache, "v9.8.7", "plugin")); err == nil {
		t.Error("the plugin directory was installed beside the binaries")
	}

	bad, asset := writeRelease(t, "9.8.7", true)
	if out, err := launch(bad, t.TempDir()); err == nil || !strings.Contains(out, "checksum mismatch for "+asset) {
		t.Fatalf("corrupt asset: err=%v out=%s", err, out)
	}
}

// scripts/install.ps1, the installer for Windows without a POSIX shell,
// installs from a local directory too.
func TestInstallPs1FromLocalMirror(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil || runtime.GOOS != "windows" {
		t.Skip("needs pwsh on Windows")
	}
	dir, _ := writeRelease(t, "9.8.7", false)
	dest := filepath.Join(t.TempDir(), "bin")
	cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-File", filepath.Join(root, "scripts", "install.ps1"), "-Version", "9.8.7", "-Dest", dest)
	cmd.Env = append(os.Environ(), "RELIAB_DOWNLOAD_URL="+dir)
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "installed reliab v9.8.7") {
		t.Fatalf("install.ps1: err=%v out=%s", err, out)
	}
	out, err := exec.Command(filepath.Join(dest, "reliab.exe"), "version").CombinedOutput()
	if err != nil {
		t.Fatalf("installed reliab: %v %s", err, out)
	}
}

// scripts/release-gate.sh passes a tag on a commit ci passed on, or on the
// release's version bump on such a commit, which may record the release in
// knowledge/log.md as past releases did (README "Release";
// knowledge/decisions/own-ci-checks.md); a bump that changes anything else
// waits for ci.
func TestReleaseGate(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s %v: %v", name, args, err)
		}
		return strings.TrimSpace(string(out))
	}
	git := func(args ...string) string {
		t.Helper()
		return run("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(files map[string]string) string {
		t.Helper()
		for path, body := range files {
			write(path, body)
		}
		git("add", "-A")
		git("commit", "-q", "-m", "c")
		return git("rev-parse", "HEAD")
	}
	gate, err := os.ReadFile(filepath.Join(root, "scripts", "release-gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	stubs := t.TempDir()
	// gh reports one successful ci run for the commit GREEN names, none else.
	if err := os.WriteFile(filepath.Join(stubs, "gh"), []byte("#!/bin/sh\ncase \"$*\" in *\"head_sha=$GREEN&\"*) echo 1 ;; *) echo 0 ;; esac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	green := commit(map[string]string{
		"scripts/release-gate.sh":           string(gate),
		"plugin/.claude-plugin/plugin.json": `{"version":"0.1.5"}`,
		"knowledge/log.md":                  "# Update Log\n",
		"main.go":                           "package main\n",
	})
	passes := func(sha string) (bool, string) {
		t.Helper()
		cmd := exec.Command(sh, filepath.Join(dir, "scripts", "release-gate.sh"), sha)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GREEN="+green, "GITHUB_REPOSITORY=o/r", "PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				t.Fatal(err)
			}
		}
		return err == nil, string(out)
	}
	if ok, out := passes(green); !ok {
		t.Errorf("a commit ci passed on was refused: %s", out)
	}
	bump := commit(map[string]string{"plugin/.claude-plugin/plugin.json": `{"version":"0.1.6"}`})
	if ok, out := passes(bump); !ok {
		t.Errorf("the version bump alone was refused: %s", out)
	}
	git("reset", "-q", "--hard", green)
	bumpLog := commit(map[string]string{"plugin/.claude-plugin/plugin.json": `{"version":"0.1.6"}`, "knowledge/log.md": "# Update Log\n\nv0.1.6\n"})
	if ok, out := passes(bumpLog); !ok {
		t.Errorf("the version bump with its log line was refused: %s", out)
	}
	git("reset", "-q", "--hard", green)
	bumpCode := commit(map[string]string{"plugin/.claude-plugin/plugin.json": `{"version":"0.1.6"}`, "main.go": "package main\n\nfunc main() {}\n"})
	if ok, out := passes(bumpCode); ok {
		t.Errorf("a bump that changes code passed: %s", out)
	} else if !strings.Contains(out, "main.go") {
		t.Errorf("the refusal does not name what else the bump changes: %s", out)
	}
}
