package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/version"
)

func run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Execute(args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

// Source: README: the commands it documents are the ones help lists, and
// the old surface is gone (knowledge/decisions/profile-v2.md, "Removed").
func TestHelpListsTheCommandSurface(t *testing.T) {
	code, out, _ := run("--help")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	for _, sub := range []string{"run", "profile", "findings", "debug", "doctor", "docs", "version"} {
		if !strings.Contains(out, "\n  "+sub+" ") {
			t.Errorf("help does not list %q", sub)
		}
	}
	for _, gone := range []string{"verify", "report", "approve", "gate", "baseline", "characterize", "hook"} {
		if strings.Contains(out, "\n  "+gone+" ") {
			t.Errorf("help still lists %q", gone)
		}
	}
}

// The version command prints what .reliab-version pins, as --version does.
func TestVersion(t *testing.T) {
	code, out, _ := run("version")
	if code != ExitOK || out != version.Version+"\n" {
		t.Fatalf("version: exit %d, stdout %q, want %q", code, out, version.Version)
	}
	if _, out, _ := run("--version"); !strings.Contains(out, version.Version) {
		t.Errorf("--version = %q", out)
	}
}

func TestUnknownCommandIsConfigError(t *testing.T) {
	if code, _, _ := run("nope"); code != ExitConfig {
		t.Fatalf("exit code = %d, want %d", code, ExitConfig)
	}
}

const runYAML = `version: 1
base: main
commands:
  build: { about: Compile., change: "true", pr: "true", audit: "true" }
  unit:
    about: Tests.
    change: echo {files} > files.txt
    audit: exit 4
    after: [build]
`

// Source: README "Run": --plan prints and runs nothing; change runs on the
// changed files; exit 0 when everything passed, 1 when something failed,
// 2 for a usage or config error.
func TestRunContextsAndExitCodes(t *testing.T) {
	root := testrepo.New(t, runYAML)
	t.Chdir(root)

	code, out, _ := run("run", "change")
	if code != ExitOK || !strings.Contains(out, "nothing changed since main") {
		t.Fatalf("no changes: exit %d, %q", code, out)
	}

	testrepo.Write(t, root, "src/a/a.txt", "changed")
	code, out, _ = run("run", "change", "--plan")
	if code != ExitOK || !strings.Contains(out, "$ echo src/a/a.txt > files.txt") {
		t.Fatalf("plan: exit %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, "files.txt")); !os.IsNotExist(err) {
		t.Fatal("--plan ran a command")
	}

	code, out, _ = run("run", "change")
	if code != ExitOK || !strings.Contains(out, "pass     unit") {
		t.Fatalf("change: exit %d\n%s", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "files.txt")); strings.TrimSpace(string(b)) != "src/a/a.txt" {
		t.Fatalf("files.txt = %q", b)
	}

	code, out, _ = run("run", "audit")
	if code != ExitFindings || !strings.Contains(out, "FAIL     unit") || !strings.Contains(out, "exit 4") {
		t.Fatalf("audit: exit %d\n%s", code, out)
	}

	code, out, errOut := run("run", "audit", "--target", "src/b", "--plan")
	if code != ExitOK || !strings.Contains(out, "target: src/b") {
		t.Fatalf("target plan: exit %d\n%s%s", code, out, errOut)
	}

	for _, args := range [][]string{{"run", "nightly"}, {"run"}, {"run", "pr", "--target", "nope"}} {
		if code, _, _ := run(args...); code != ExitConfig {
			t.Errorf("%v: exit %d, want %d", args, code, ExitConfig)
		}
	}
}

// Source: README "Run": with no reliab.yaml there is nothing to run, a
// config error.
func TestRunWithoutConfig(t *testing.T) {
	t.Chdir(testrepo.New(t, ""))
	if code, _, errOut := run("run", "pr"); code != ExitConfig || !strings.Contains(errOut, "reliab.yaml") {
		t.Fatalf("exit %d, %q", code, errOut)
	}
}

// Source: knowledge/decisions/profile-v2.md, "Removed": profile migrate
// converts a reliability-profile.yaml, and refuses to overwrite.
func TestProfileMigrateAndCheck(t *testing.T) {
	root := testrepo.New(t, "")
	testrepo.Write(t, root, "reliability-profile.yaml", `schemaVersion: 0.1.0
commands:
  build: go build ./...
  unit: go test {impacted}
impacted: { all: ./... }
debug:
  adapter: { run: dlv dap, transport: stdio }
`)
	t.Chdir(root)
	code, out, errOut := run("profile", "migrate")
	if code != ExitOK || !strings.Contains(out, "wrote reliab.yaml") || !strings.Contains(out, "wrote reliab-debug.yaml") {
		t.Fatalf("migrate: exit %d\n%s%s", code, out, errOut)
	}
	if code, out, _ := run("profile", "check"); code != ExitOK || out != "reliab.yaml: 2 commands, ok\n" {
		t.Fatalf("check: exit %d %q", code, out)
	}
	if code, _, errOut := run("profile", "migrate"); code != ExitConfig || !strings.Contains(errOut, "--force") {
		t.Fatalf("second migrate: exit %d %q", code, errOut)
	}
	if code, _, _ := run("profile", "migrate", "--force"); code != ExitOK {
		t.Fatalf("--force: exit %d", code)
	}
}
