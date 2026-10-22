package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// reliab snapdiff is the snapshot rule for adapters in any language.
func TestSnapdiffHelper(t *testing.T) {
	dir := t.TempDir()
	testrepo.Write(t, dir, "a.verified.txt", "{\n  A: 1\n}\n")
	testrepo.Write(t, dir, "a.received.txt", "{\n  A: 1,\n  B: 2\n}\n")
	testrepo.Write(t, dir, "b.received.txt", "{\n  A: 2\n}\n")
	acc := filepath.Join(dir, "a.verified.txt")

	code, out, _ := run("snapdiff", acc, filepath.Join(dir, "a.received.txt"))
	if code != ExitOK || !strings.HasPrefix(out, "additive-only +1 -0\n") {
		t.Errorf("text: code %d out %q", code, out)
	}
	code, out, _ = run("snapdiff", "--json", acc, filepath.Join(dir, "b.received.txt"))
	var got struct {
		Label   string `json:"label"`
		Added   int    `json:"added"`
		Removed int    `json:"removed"`
		New     bool   `json:"new"`
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &got) != nil || got.Label != "values-changed" || got.Added != 1 || got.Removed != 1 || got.New {
		t.Errorf("json: code %d out %q", code, out)
	}
	if _, out, _ = run("snapdiff", acc, acc); strings.TrimSpace(out) != "equal" {
		t.Errorf("equal files: %q", out)
	}
	if code, _, _ = run("snapdiff", acc, filepath.Join(dir, "missing")); code != ExitConfig {
		t.Errorf("unreadable pending file: code %d", code)
	}
}

// reliab changed-lines prints the lines changed since the profile's base.
func TestChangedLinesHelper(t *testing.T) {
	root := testrepo.New(t, "base: main\ncommands:\n  build: \"true\"\n")
	testrepo.Git(t, root, "checkout", "-q", "-b", "topic")
	testrepo.Write(t, root, "src/a/a.txt", "a\nb\nc\n")
	t.Chdir(root)
	code, out, errOut := run("changed-lines")
	if code != ExitOK || out != "src/a/a.txt: 1-3\n" {
		t.Errorf("text: code %d out %q err %q", code, out, errOut)
	}
	code, out, _ = run("changed-lines", "--json")
	var got struct {
		Base  string              `json:"base"`
		Files map[string][][2]int `json:"files"`
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &got) != nil || got.Base != "main" || len(got.Files["src/a/a.txt"]) != 1 || got.Files["src/a/a.txt"][0] != [2]int{1, 3} {
		t.Errorf("json: code %d out %q", code, out)
	}
	if code, _, _ = run("changed-lines", "--base", "nosuch"); code != ExitConfig {
		t.Errorf("unknown base: code %d", code)
	}
	// Nothing changed: stdout stays empty for scripts, stderr says why.
	t.Chdir(testrepo.New(t, "commands:\n  build: \"true\"\n"))
	if code, out, errOut = run("changed-lines"); code != ExitOK || out != "" || !strings.Contains(errOut, "no lines changed since main") {
		t.Errorf("no changes: code %d out %q err %q", code, out, errOut)
	}
}

func TestToRanges(t *testing.T) {
	got := toRanges(map[int]bool{5: true, 1: true, 2: true, 3: true, 9: true})
	want := [][2]int{{1, 3}, {5, 5}, {9, 9}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("range %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// reliab findings check lets an adapter's author see its findings file as
// verify will, before wiring it: 0 clean, 1 warnings, 2 rejected.
func TestFindingsCheckHelper(t *testing.T) {
	root := testrepo.New(t, "commands:\n  build: \"true\"\n")
	t.Chdir(root)
	testrepo.Write(t, root, "ok.json", `{"schemaVersion":"0.4.0","mutants":{"killed":1,"survived":1},"findings":[{"test":"src/a/a.txt: negate","location":"src/a/a.txt:1:1"}]}`)
	testrepo.Write(t, root, "warn.json", `{"schemaVersion":"0.4.0","findings":[{"test":"a.txt:1: negate","location":"a.txt:1:1"}]}`)
	testrepo.Write(t, root, "bad.json", `{"schemaVersion":"0.4.0","findings":[{"test":"a"},{"test":"a"}]}`)

	code, out, errOut := run("findings", "check", "ok.json")
	if code != ExitOK || !strings.Contains(out, "ok.json: 1 findings\n  src/a/a.txt: negate  (src/a/a.txt:1:1)\n") || !strings.Contains(out, "mutants: 1 killed, 1 survived") {
		t.Errorf("clean: code %d out %q err %q", code, out, errOut)
	}
	if strings.Contains(out, "paths not checked") {
		t.Errorf("in a repository: %q", out)
	}
	code, out, _ = run("findings", "check", "warn.json")
	if code != ExitFindings || strings.Count(out, "warning:") != 2 || !strings.Contains(out, "names a line") || !strings.Contains(out, "a.txt is not a file in the repository") {
		t.Errorf("warnings: code %d out %q", code, out)
	}
	if code, _, errOut = run("findings", "check", "bad.json"); code != ExitConfig || !strings.Contains(errOut, "verify would reject it") {
		t.Errorf("rejected: code %d err %q", code, errOut)
	}
	if code, _, _ = run("findings", "check", "missing.json"); code != ExitConfig {
		t.Errorf("missing file: code %d", code)
	}
}

// Outside a repository the paths cannot be checked, and the output says so
// rather than passing them silently.
func TestFindingsCheckOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	testrepo.Write(t, dir, "ok.json", `{"schemaVersion":"0.4.0","findings":[{"test":"x","location":"nowhere.rs:1"}]}`)
	if code, out, _ := run("findings", "check", "ok.json"); code != ExitOK || !strings.Contains(out, "paths not checked") {
		t.Errorf("code %d out %q", code, out)
	}
}

// reliab docs adapters prints the adapter contract for the installed
// version, which the adapter skill reads in a repository without docs/.
func TestDocsAdapters(t *testing.T) {
	code, out, _ := run("docs", "adapters")
	if code != ExitOK || !strings.HasPrefix(out, "# Writing an adapter\n") || !strings.Contains(out, "reliab findings check") {
		t.Errorf("code %d out %.80q", code, out)
	}
	if code, out, _ := run("docs"); code != ExitOK || out != "topics: adapters, ci, debug, overview\n" {
		t.Errorf("no topic: code %d out %q", code, out)
	}
	if code, out, _ := run("docs", "overview"); code != ExitOK || !strings.HasPrefix(out, "# reliab in this repository\n") {
		t.Errorf("overview: code %d out %.80q", code, out)
	}
	if code, out, _ := run("docs", "ci"); code != ExitOK || !strings.Contains(out, "reliab gate protected") {
		t.Errorf("ci: code %d out %.80q", code, out)
	}
	// knowledge/decisions/debug-adapter-seam.md: reliab docs debug prints
	// the debug page.
	if code, out, _ := run("docs", "debug"); code != ExitOK || !strings.HasPrefix(out, "# Debugging with reliab\n") || !strings.Contains(out, "reliab debug probe") {
		t.Errorf("debug: code %d out %.80q", code, out)
	}
	if code, _, errOut := run("docs", "nosuch"); code != ExitConfig || !strings.Contains(errOut, "topics: adapters, ci, debug, overview") {
		t.Errorf("unknown topic: code %d err %q", code, errOut)
	}
}
