package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// A per-package run also mutates the changed files of a package nested
// under it (a prepr run in internal/guard reported effects/lex.go), and
// against the wrong tests reports them not covered. The adapter's contract
// (header comment, knowledge/decisions/external-adapters.md: gremlins runs
// per package) is that a package's mutants come from its own run, so the
// nested file is dropped here and counted only from its own report.
func TestConvertDropsNestedPackages(t *testing.T) {
	const parent = `{"go_module":"example.com/m","files":[
  {"file_name":"guard.go","mutations":[{"type":"CONDITIONALS_NEGATION","status":"LIVED","line":5,"column":2}]},
  {"file_name":"effects/lex.go","mutations":[{"type":"ARITHMETIC_BASE","status":"NOT COVERED","line":7,"column":3}]}]}`
	got, counts, err := convert([]byte(parent), "internal/guard", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].File != "internal/guard/guard.go" || counts != (findings.MutantCounts{Survived: 1}) {
		t.Errorf("mutants = %+v, counts %+v; want only guard.go's", got, counts)
	}
	// A report with no DIR is a module-root run: its paths are the module's.
	if got, _, _ := convert([]byte(parent), "", nil); len(got) != 2 {
		t.Errorf("module-root report kept %d mutants, want 2", len(got))
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

// knowledge/decisions/mutation-threshold.md: timeouts count as killed, never
// as survivors. A gremlins report whose tests timed out on every mutant has
// no survivor findings, and its counts, in total and per file, are kills,
// so the score reads them as caught.
func TestTimedOutMutantsAreKilledNotSurvivors(t *testing.T) {
	const timedOut = `{"go_module":"example.com/m","files":[{"file_name":"lock.go","mutations":[
  {"type":"CONDITIONALS_NEGATION","status":"TIMED OUT","line":12,"column":5},
  {"type":"ARITHMETIC_BASE","status":"TIMED OUT","line":30,"column":9}]}],
  "test_efficacy":100,"mutants_total":2}`
	dir := t.TempDir()
	rep := filepath.Join(dir, "g.json")
	if err := os.WriteFile(rep, []byte(timedOut), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "findings.json")
	if err := run("", out, []string{"internal/verify=" + rep}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapters.ParseFindings(raw)
	if err != nil {
		t.Fatalf("reliab cannot read %s: %v", raw, err)
	}
	if len(got) != 0 {
		t.Errorf("findings = %+v, want no survivors", got)
	}
	if c := adapters.FindingsCounts(raw); c == nil || c.Killed != 2 || c.Survived != 0 || c.NotCovered != 0 {
		t.Errorf("counts = %+v, want 2 killed", c)
	}
	if files := adapters.FindingsFileCounts(raw); files["internal/verify/lock.go"].Killed != 2 || files["internal/verify/lock.go"].Survived != 0 {
		t.Errorf("file counts = %+v, want 2 killed in internal/verify/lock.go", files)
	}
}

// knowledge/decisions/external-adapters.md: a mutant in a file this platform
// does not build is not a survivor here, since no test here can compile it;
// it is dropped and not counted, and the platform that builds the file
// measures it.
func TestMutantsInFilesThisPlatformDoesNotBuildAreDropped(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("here.go", "package p\n")
	write("elsewhere.go", "//go:build !"+runtime.GOOS+"\n\npackage p\n")
	raw := `{"go_module":"example.com/m","files":[
  {"file_name":"elsewhere.go","mutations":[
    {"type":"CONDITIONALS_NEGATION","status":"NOT COVERED","line":3,"column":1},
    {"type":"CONDITIONALS_NEGATION","status":"LIVED","line":3,"column":2}]},
  {"file_name":"here.go","mutations":[
    {"type":"CONDITIONALS_NEGATION","status":"NOT COVERED","line":1,"column":1}]}]}`
	got, counts, err := convert([]byte(raw), filepath.ToSlash(dir), nil)
	if err != nil {
		t.Fatal(err)
	}
	if counts != (findings.MutantCounts{NotCovered: 1}) {
		t.Errorf("counts = %+v, want only here.go's mutant", counts)
	}
	if len(got) != 1 || !strings.HasSuffix(got[0].File, "/here.go") {
		t.Errorf("mutants = %+v, want only here.go's", got)
	}
}
