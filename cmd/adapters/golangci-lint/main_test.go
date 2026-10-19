package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/pkg/findings"
)

// The fixtures are trimmed from real golangci-lint v2.15.0 runs on Windows
// over a package with planted issues (testdata/*.json): clean; failing
// (two identical errcheck issues in one file, a govet and two unused);
// moved (the same, four lines lower); broken (a compile error, exit 7, an
// empty Issues list). failing-windows-abs and moved-slash are the same runs
// with absolute Windows paths and with Linux separators.

// adapt runs the adapter on a fixture with an exit code and returns the
// findings it wrote, or ok false when it wrote none.
func adapt(t *testing.T, exit, fixture, root string) ([]findings.Finding, bool, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "findings.json")
	var stderr bytes.Buffer
	code := cli([]string{"-exit", exit, "-root", root, filepath.Join("testdata", fixture), out}, &stderr)
	raw, err := os.ReadFile(out)
	if code != 0 || err != nil {
		if err == nil {
			t.Errorf("%s: exit %d but a file was written", fixture, code)
		}
		return nil, false, stderr.String()
	}
	var doc struct {
		SchemaVersion string             `json:"schemaVersion"`
		Findings      []findings.Finding `json:"findings"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.SchemaVersion != findings.SchemaVersion || doc.Findings == nil {
		t.Fatalf("%s: not a findings file: %s", fixture, raw)
	}
	return doc.Findings, true, stderr.String()
}

func tests(fs []findings.Finding) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Test
	}
	return out
}

// docs/adapters.md: the tool ran clean, so the file has no findings.
func TestClean(t *testing.T) {
	if fs, ok, errOut := adapt(t, "0", "clean.json", "."); !ok || len(fs) != 0 {
		t.Errorf("clean: %v %v %s", fs, ok, errOut)
	}
}

// docs/adapters.md: one finding per issue, its test the repo-relative
// file, linter, and message without positions, numbered when two are the
// same; the location keeps file:line:col.
func TestFailing(t *testing.T) {
	fs, ok, errOut := adapt(t, "1", "failing.json", ".")
	if !ok {
		t.Fatalf("failing: %s", errOut)
	}
	want := []string{
		"internal/okf/planted.go: unused: func plantedA is unused",
		"internal/okf/planted.go: errcheck: Error return value of `os.Remove` is not checked",
		"internal/okf/planted.go: errcheck: Error return value of `os.Remove` is not checked (#2)",
		"internal/okf/planted.go: unused: func plantedB is unused",
		"internal/okf/planted.go: govet: assign: self-assignment of x",
	}
	if !slices.Equal(tests(fs), want) {
		t.Fatalf("tests = %q", tests(fs))
	}
	if fs[1].Location != "internal/okf/planted.go:6:11" || fs[2].Location != "internal/okf/planted.go:7:11" {
		t.Errorf("locations = %q, %q", fs[1].Location, fs[2].Location)
	}
	if !strings.HasPrefix(fs[1].Message, "errcheck: Error return value") || !strings.Contains(fs[1].Message, `os.Remove("a")`) {
		t.Errorf("message = %q", fs[1].Message)
	}
}

// docs/adapters.md: a finding keeps its id when code above it moves, and
// on any OS: Windows backslashes, absolute paths under the root, and Linux
// slashes give the same tests.
func TestIdsHoldAcrossMovesAndPaths(t *testing.T) {
	base, _, _ := adapt(t, "1", "failing.json", ".")
	for _, c := range []struct{ fixture, root string }{
		{"moved.json", "."},
		{"moved-slash.json", "."},
		{"failing-windows-abs.json", `C:\Users\ci\src\reliab`},
		{"failing-windows-abs.json", "c:/users/ci/src/reliab/"},
	} {
		fs, ok, errOut := adapt(t, "1", c.fixture, c.root)
		if !ok || !slices.Equal(tests(fs), tests(base)) {
			t.Errorf("%s under %s: %q %s", c.fixture, c.root, tests(fs), errOut)
		}
		if ok && !strings.HasPrefix(fs[0].Location, "internal/okf/planted.go:") {
			t.Errorf("%s: location %q", c.fixture, fs[0].Location)
		}
	}
	moved, _, _ := adapt(t, "1", "moved.json", ".")
	if moved[1].Location != "internal/okf/planted.go:10:11" {
		t.Errorf("moved location = %q", moved[1].Location)
	}
}

// pkg/exitcodes in golangci-lint v2.15.0: only 0 and 1 mean it ran. A
// compile error exits 7 with an empty Issues list, and a config error
// exits 3 with no file; either writes nothing, so reliab reports the
// command as failed instead of reading an empty file as a pass.
func TestFailedRunsWriteNothing(t *testing.T) {
	for _, c := range []struct{ exit, fixture, want string }{
		{"7", "broken.json", "exited 7"},
		{"3", "missing.json", "exited 3"},
		{"0", "missing.json", "missing.json"},
		{"1", "clean.json", "exited 1 but"},
		{"1", "../main.go", "not golangci-lint JSON"},
	} {
		if fs, ok, errOut := adapt(t, c.exit, c.fixture, "."); ok || !strings.Contains(errOut, c.want) {
			t.Errorf("exit %s %s: wrote %v; stderr %q", c.exit, c.fixture, fs, errOut)
		}
	}
	if code := cli([]string{"only-one-arg"}, &bytes.Buffer{}); code != 2 {
		t.Errorf("usage: exit %d", code)
	}
}

func TestWithoutPositions(t *testing.T) {
	for in, want := range map[string]string{
		"x declared and not used":                      "x declared and not used",
		"other declaration of x at a.go:12:3":          "other declaration of x at a.go",
		"shadows declaration at line 12":               "shadows declaration at line",
		"function is too long (Column 80 > 120)":       "function is too long (Column > 120)",
		"cyclomatic complexity 31 of func `f` is high": "cyclomatic complexity 31 of func `f` is high",
	} {
		if got := withoutPositions(in); got != want {
			t.Errorf("withoutPositions(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepoPath(t *testing.T) {
	for _, c := range []struct{ root, p, want string }{
		{"/src/r", "internal/a.go", "internal/a.go"},
		{"/src/r", `internal\a.go`, "internal/a.go"},
		{"/src/r", "/src/r/internal/a.go", "internal/a.go"},
		{`C:\src\r`, `C:\src\r\internal\a.go`, "internal/a.go"},
		{`C:\src\r`, `c:\SRC\r\internal\a.go`, "internal/a.go"},
		{"/src/r", "/src/rx/a.go", "/src/rx/a.go"},
		{"/src/r", "/elsewhere/a.go", "/elsewhere/a.go"},
	} {
		if got := repoPath(c.root, c.p); got != c.want {
			t.Errorf("repoPath(%q, %q) = %q, want %q", c.root, c.p, got, c.want)
		}
	}
}
