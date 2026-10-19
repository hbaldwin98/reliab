package repocheck

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
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
