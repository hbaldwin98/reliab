package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/adapters"
	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/pkg/findings"
)

// Trimmed from a real gremlins 0.6.0 run on one package, so file names are
// relative to it.
const sample = `{"go_module":"example.com/m","files":[{"file_name":"triage.go","mutations":[
  {"type":"CONDITIONALS_NEGATION","status":"KILLED","line":69,"column":24},
  {"type":"CONDITIONALS_NEGATION","status":"LIVED","line":136,"column":15},
  {"type":"CONDITIONALS_NEGATION","status":"LIVED","line":132,"column":10},
  {"type":"CONDITIONALS_BOUNDARY","status":"NOT COVERED","line":145,"column":16},
  {"type":"ARITHMETIC_BASE","status":"TIMED OUT","line":118,"column":25},
  {"type":"ARITHMETIC_BASE","status":"SKIPPED","line":119,"column":2}]}],
  "test_efficacy":75,"mutants_total":6}`

func TestConvert(t *testing.T) {
	got, counts, err := convert([]byte(sample), "internal/triage", nil)
	if err != nil {
		t.Fatal(err)
	}
	// knowledge/decisions/pr-evidence-summary.md: the findings file counts the
	// mutants tested, after the changed-lines filter, so the PR summary can say
	// tested, killed, and survived.
	// Timeouts count as caught; SKIPPED was never tested.
	if counts != (findings.MutantCounts{Killed: 2, Survived: 2, NotCovered: 1}) {
		t.Errorf("counts = %+v", counts)
	}
	if len(got) != 3 {
		t.Fatalf("mutants = %+v, want 2 lived and 1 not covered", got)
	}
	if got[0].File != "internal/triage/triage.go" || got[0].Line != 136 {
		t.Errorf("first = %+v", got[0])
	}
	if got[2].Detail != "no test covers this line" {
		t.Errorf("not covered = %+v", got[2])
	}
	keep := func(file string, line int) bool { return file == "internal/triage/triage.go" && line == 132 }
	if got, counts, _ := convert([]byte(sample), "internal/triage", keep); len(got) != 1 || got[0].Line != 132 || counts != (findings.MutantCounts{Survived: 1}) {
		t.Errorf("filtered = %+v, counts %+v", got, counts)
	}
	if _, _, err := convert([]byte(`{"files":[]}`), "", nil); err == nil {
		t.Error("want error without go_module")
	}
}

func TestParseChanged(t *testing.T) {
	got, err := parseChanged([]byte(`{"base":"main","files":{"a.go":[[3,3],[11,12]]}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]bool{3: true, 11: true, 12: true}
	if len(got) != 1 || len(got["a.go"]) != len(want) {
		t.Fatalf("got %v, want a.go %v", got, want)
	}
	for l := range want {
		if !got["a.go"][l] {
			t.Errorf("line %d not changed", l)
		}
	}
	if _, err := parseChanged([]byte("no")); err == nil {
		t.Error("want error for output that is not JSON")
	}
}

// End to end in a git repository: per-package reports, filtered to the
// branch's changed lines, written as findings reliab can read.
func TestRun(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	src := filepath.Join(root, "internal", "triage", "triage.go")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 150)
	for i := range lines {
		lines[i] = "// line"
	}
	write := func() {
		if err := os.WriteFile(src, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write()
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-q", "-m", "base")
	lines[131] = "// changed" // line 132
	write()

	rep := filepath.Join(root, "g.json")
	if err := os.WriteFile(rep, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "findings.json")
	t.Chdir(root)
	// reliab changed-lines prints gitx.ChangedLines; call it directly.
	orig := changedSince
	t.Cleanup(func() { changedSince = orig })
	changedSince = func(base string) (map[string]map[int]bool, error) { return gitx.ChangedLines(root, base) }
	if err := run("main", out, []string{"internal/triage=" + rep}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)
	got, err := adapters.ParseFindings(raw)
	if err != nil {
		t.Fatalf("reliab cannot read %s: %v", raw, err)
	}
	if len(got) != 1 || got[0].Location != "internal/triage/triage.go:132:10" {
		t.Fatalf("findings = %+v, want the lived mutant on the changed line", got)
	}
	// The counts follow the changed-lines filter (pr-evidence-summary.md).
	if c := adapters.FindingsCounts(raw); c == nil || c.Killed != 0 || c.Survived != 1 || c.NotCovered != 0 {
		t.Errorf("counts = %+v, want the one mutant on the changed line", c)
	}
	// And per file, so a baseline scope can be scored (mutation-threshold.md).
	if files := adapters.FindingsFileCounts(raw); len(files) != 1 || files["internal/triage/triage.go"].Survived != 1 || files["internal/triage/triage.go"].Killed != 0 {
		t.Errorf("file counts = %+v, want the one mutant in internal/triage/triage.go", files)
	}

	missing := filepath.Join(root, "f2.json")
	if err := run("", missing, []string{filepath.Join(root, "nope.json")}); err == nil {
		t.Fatal("want error for a missing report")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("output written on error: %v", err)
	}
}

func TestCLI(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	if code := cli(nil, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage") {
		t.Errorf("no args: code %d, stderr %q", code, stderr.String())
	}
	if code := cli([]string{"-nope"}, &stderr); code != 2 {
		t.Errorf("bad flag: code %d", code)
	}
	stderr.Reset()
	if code := cli([]string{filepath.Join(dir, "f.json"), filepath.Join(dir, "missing.json")}, &stderr); code != 1 || !strings.Contains(stderr.String(), "reliab-adapter-gremlins:") {
		t.Errorf("missing report: code %d, stderr %q", code, stderr.String())
	}
	// No reports: the change touched no package, and that is a clean result.
	out := filepath.Join(dir, "empty.json")
	if code := cli([]string{out}, &stderr); code != 0 {
		t.Fatalf("no reports: code %d, stderr %q", code, stderr.String())
	}
	raw, _ := os.ReadFile(out)
	if got, err := adapters.ParseFindings(raw); err != nil || len(got) != 0 {
		t.Errorf("no reports: %v, %+v", err, got)
	}
}

// With -base the adapter needs reliab changed-lines; when it cannot run, the
// adapter fails rather than keeping every mutant (package doc: an unreadable
// input exits 1 and writes nothing).
func TestChangedSinceNeedsReliab(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := changedSince("main"); err == nil || !strings.Contains(err.Error(), "reliab changed-lines") {
		t.Errorf("err = %v, want the failed helper named", err)
	}
}
