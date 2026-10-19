// Package repocheck holds tests that keep the repository's non-Go parts
// consistent: the marketplace, the plugin, and the install scripts.
package repocheck

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/cli"
)

const root = "../.."

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestMarketplaceListsPlugin(t *testing.T) {
	var m struct {
		Name    string `json:"name"`
		Owner   struct{ Name string }
		Plugins []struct{ Name, Source, Description string }
	}
	readJSON(t, ".claude-plugin/marketplace.json", &m)
	if m.Name != "reliab" || m.Owner.Name == "" || len(m.Plugins) != 1 {
		t.Fatalf("marketplace = %+v", m)
	}
	if p := m.Plugins[0]; p.Name != "reliab" || p.Source != "./plugin" || p.Description == "" {
		t.Errorf("plugin entry = %+v", p)
	}
}

func TestPluginVersionIsSemver(t *testing.T) {
	var p struct{ Name, Version string }
	readJSON(t, "plugin/.claude-plugin/plugin.json", &p)
	if p.Name != "reliab" || !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(p.Version) {
		t.Fatalf("plugin.json = %+v", p)
	}
}

// TestHooksCallRealSubcommands keeps hooks.json and the CLI in step.
func TestHooksCallRealSubcommands(t *testing.T) {
	var h struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string }
		}
	}
	readJSON(t, "plugin/hooks/hooks.json", &h)
	re := regexp.MustCompile(`/bin/reliab" (hook [a-z-]+)$`)
	n := 0
	for event, groups := range h.Hooks {
		for _, g := range groups {
			for _, hk := range g.Hooks {
				m := re.FindStringSubmatch(hk.Command)
				if m == nil {
					t.Errorf("%s: command %q does not call the reliab launcher", event, hk.Command)
					continue
				}
				var out, errOut bytes.Buffer
				args := append(strings.Fields(m[1]), "--help")
				if code := cli.Execute(args, strings.NewReader(""), &out, &errOut); code != 0 {
					t.Errorf("%s: reliab %s is not a command: %s", event, m[1], errOut.String())
				}
				n++
			}
		}
	}
	if n < 4 {
		t.Errorf("only %d hooks configured", n)
	}
}

func TestInstallScriptsMatch(t *testing.T) {
	a, err := os.ReadFile(filepath.Join(root, "scripts/install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "plugin/bin/reliab-install"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("scripts/install.sh and plugin/bin/reliab-install differ; keep them identical")
	}
}

func TestLauncherRunsDevBinary(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	bin := filepath.Join(t.TempDir(), "reliab")
	if b, err := exec.Command("go", "build", "-o", bin, "../../cmd/reliab").CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	if _, err := os.Stat(bin + ".exe"); err == nil {
		bin += ".exe"
	}
	cmd := exec.Command(sh, filepath.Join(root, "plugin/bin/reliab"), "--version")
	cmd.Env = append(os.Environ(), "RELIAB_BIN="+bin)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "reliab version") {
		t.Fatalf("launcher: %v %s", err, out)
	}
}

// TestLauncherUnreleasedUsesDevBuild covers plugin version 0.0.0: the
// launcher runs ~/.reliab/dev/reliab and never tries to download.
func TestLauncherUnreleasedUsesDevBuild(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	home := t.TempDir()
	launch := func() (string, error) {
		cmd := exec.Command(sh, filepath.Join(root, "plugin/bin/reliab"), "--version")
		env := []string{"HOME=" + home, "RELIAB_VERSION=0.0.0"}
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "HOME=") && !strings.HasPrefix(kv, "RELIAB_") {
				env = append(env, kv)
			}
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := launch(); err == nil || !strings.Contains(out, "unreleased plugin") {
		t.Fatalf("without a dev build: err=%v out=%s", err, out)
	}
	dev := filepath.Join(home, ".reliab", "dev", "reliab")
	if runtime.GOOS == "windows" {
		dev += ".exe"
	}
	if b, err := exec.Command("go", "build", "-o", dev, "../../cmd/reliab").CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	if out, err := launch(); err != nil || !strings.Contains(out, "reliab version") {
		t.Fatalf("with a dev build: err=%v out=%s", err, out)
	}
}
