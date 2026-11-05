package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/verify"
)

// TestPrintPlanExact pins the --plan text. Source: README "Run" and
// knowledge/decisions/profile-v2.md "Plan and schedule": --plan prints the
// target, what would run in order with each filled line and about, and
// what does not run and why.
func TestPrintPlanExact(t *testing.T) {
	cfg, err := Parse([]byte(`version: 1
commands:
  build: { about: Compile., pr: make }
  unit:
    about: |
      Tests.
      Fix the code.
    pr: test {projects}
    after: [build]
    timeout: 90s
  mut: { about: Mutants., pr: 'mutate {files}', after: [unit], exclusive: true }
  e2e: { about: E2E., pr: 'e2e {tests}' }
  fuzz: { about: Fuzz., audit: fuzz }
`))
	if err != nil {
		t.Fatal(err)
	}
	tg := &Target{Specs: []string{"a.go#F", "b.go"}, Files: []string{"a.go", "b.go"}, Projects: []string{"./"},
		Ranges: map[string][][2]int{"a.go": {{3, 9}, {12, 12}}}}
	p, err := Build(cfg, "pr", tg, true, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	p.Print(&b)
	want := `reliab run pr
target: a.go#F, b.go
  files: 2   projects: ./   tests: none
  a.go lines 3-9,12
base: origin/main

1.
  build
    $ make
    Compile.
2.
  unit  (after build; timeout 1m30s)
    $ test ./
    Tests.
    Fix the code.
3.
  mut  (runs alone; after unit)
    $ mutate a.go b.go
    Mutants.

skipped, target gives them nothing to run on: e2e
not in pr: fuzz
`
	if b.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

// Source: README "Run": a plan with nothing to run says so; an audit of
// everything prints no file counts. listOrNone keeps long lists short.
func TestPrintPlanEmptyAndEverything(t *testing.T) {
	cfg, _ := Parse([]byte("version: 1\ncommands:\n  a: { about: x, pr: a }\n"))
	p, _ := Build(cfg, "audit", ResolveAll(), false, "")
	var b bytes.Buffer
	p.Print(&b)
	if b.String() != "reliab run audit\ntarget: everything\n\nnothing to run\nnot in audit: a\n" {
		t.Fatalf("got %q", b.String())
	}
	if got := listOrNone([]string{"1", "2", "3", "4", "5", "6"}); got != "1 2 3 4 (+2)" {
		t.Fatalf("listOrNone = %q", got)
	}
	if got := listOrNone([]string{"1", "2", "3", "4"}); got != "1 2 3 4" {
		t.Fatalf("listOrNone = %q", got)
	}
}

// Source: README "Run" shows changed line ranges; more than five files'
// ranges would bury the plan, so they are left out.
func TestPrintPlanManyRangesOmitted(t *testing.T) {
	tg := &Target{Specs: []string{"x"}, Ranges: map[string][][2]int{}}
	for _, f := range []string{"a", "b", "c", "d", "e", "f"} {
		tg.Files = append(tg.Files, f)
		tg.Ranges[f] = [][2]int{{1, 1}}
	}
	cfg, _ := Parse([]byte("version: 1\ncommands:\n  a: { about: x, pr: a }\n"))
	p, _ := Build(cfg, "pr", tg, false, "")
	var b bytes.Buffer
	p.Print(&b)
	if strings.Contains(b.String(), " lines ") {
		t.Fatalf("six files' ranges printed:\n%s", b.String())
	}
	tg.Files, tg.Ranges = tg.Files[:5], map[string][][2]int{"a": {{1, 1}}, "b": {{1, 1}}, "c": {{1, 1}}, "d": {{1, 1}}, "e": {{2, 4}}}
	b.Reset()
	p.Print(&b)
	if !strings.Contains(b.String(), "  e lines 2-4\n") {
		t.Fatalf("five files' ranges must print:\n%s", b.String())
	}
}

// Source: profile-v2.md "Targets": placeholders fill "already-resolved
// lists" as shell words; a word that needs no quoting is left bare.
func TestShellWords(t *testing.T) {
	got := shellWords([]string{"src/a.go", "./pkg", "Api.Tests.csproj", "x#F:1", "AZaz09_-", "a b", "it's", "", "$x", "[", "@", "{", "`"})
	want := `src/a.go ./pkg Api.Tests.csproj x#F:1 AZaz09_- 'a b' 'it'\''s' '' '$x' '[' '@' '{' '` + "`'"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// Source: README "Run": a function target is found by parsing the file;
// brace languages by a tolerant scanner that is not fooled by braces in
// comments or strings, and a single-line body ends on its line.
func TestFindFuncScannerEdges(t *testing.T) {
	src := strings.Join([]string{
		"class A {",
		"  void B() {",
		`    var s = "a \" } still string";`,
		"    /* } still",
		"       comment } */ var c = '}';",
		"    // }",
		"  }",
		"  int E() =>",
		"    1 +",
		"    2; // done",
		"  void F()",
		"",
		"",
		"",
		"  {",
		"  }",
		"  void G() {}",
		"}",
	}, "\n")
	cases := map[string][2]int{"B": {2, 7}, "E": {8, 10}, "G": {17, 17}}
	for name, want := range cases {
		s, e, err := FindFunc("A.cs", []byte(src), name)
		if err != nil || s != want[0] || e != want[1] {
			t.Errorf("%s: got %d-%d %v, want %d-%d", name, s, e, err, want[0], want[1])
		}
	}
	// F's body opens four lines below its signature: the scanner gives up
	// rather than claim a brace that belongs to something else.
	if _, _, err := FindFunc("A.cs", []byte(src), "F"); err == nil {
		t.Error("F: want no match, its body is too far from the declaration")
	}
	// An expression body with no ';' and an unclosed body are not functions.
	for _, s := range []string{"int H() =>\n  1\n", "void H() {\n  x();\n"} {
		if _, _, err := FindFunc("A.cs", []byte(s), "H"); err == nil {
			t.Errorf("%q: want no match", s)
		}
	}
}

// Source: README "reliab.yaml" ({tests}: test_foo.py beside foo.py) and
// profile-v2.md "Targets" (a project file: package.json, go.mod).
func TestResolveProjectMarkersAndPythonTests(t *testing.T) {
	dir := testrepo.New(t, "")
	testrepo.Write(t, dir, "py/pyproject.toml", "")
	testrepo.Write(t, dir, "py/m/calc.py", "def f():\n    pass\n")
	testrepo.Write(t, dir, "py/m/test_calc.py", "")
	testrepo.Write(t, dir, "py/m/calc_test.py", "")
	testrepo.Write(t, dir, "js/package.json", "{}")
	testrepo.Write(t, dir, "js/x.js", "")
	testrepo.Write(t, dir, "mod/go.mod", "module m\n")
	testrepo.Write(t, dir, "mod/a.go", "package m\n")
	testrepo.Git(t, dir, "add", "-A")
	testrepo.Git(t, dir, "commit", "-qm", "x")

	tg, err := Resolve(dir, []string{filepath.Join(dir, "py/m/calc.py")})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tg.Projects, ",") != "py" || strings.Join(tg.Tests, ",") != "py/m/calc_test.py,py/m/test_calc.py" {
		t.Fatalf("projects=%v tests=%v", tg.Projects, tg.Tests)
	}
	for spec, files := range map[string]string{"js/package.json": "js/package.json,js/x.js", "mod/go.mod": "mod/a.go,mod/go.mod"} {
		tg, err := Resolve(dir, []string{filepath.Join(dir, spec)})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(tg.Files, ",") != files {
			t.Errorf("%s: files = %v, want the project's whole directory", spec, tg.Files)
		}
	}
	tg, err = Resolve(dir, []string{filepath.Join(dir, "src/a/a.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tg.Projects) != 0 || strings.Join(tg.Files, ",") != "src/a/a.txt" {
		t.Fatalf("a file under no project: files=%v projects=%v", tg.Files, tg.Projects)
	}
}

// Source: profile-v2.md "Targets": change and pr default to changed lines,
// as ranges; a run of changed lines is one range.
func TestToRanges(t *testing.T) {
	got := toRanges(map[int]bool{7: true, 2: true, 3: true, 4: true, 9: true, 10: true})
	if len(got) != 3 || got[0] != [2]int{2, 4} || got[1] != [2]int{7, 7} || got[2] != [2]int{9, 10} {
		t.Fatalf("got %v", got)
	}
}

// Source: profile-v2.md "Plan and schedule": a step whose after failed is
// blocked, a passing after lets it start, and --parallel caps how many run
// at once (README "Run"; reliab run --parallel).
func TestRunParallelCap(t *testing.T) {
	shell, err := verify.FindShell()
	if err != nil {
		t.Skip(err)
	}
	y := "version: 1\ncommands:\n"
	for _, n := range []string{"a", "b", "c"} {
		y += "  " + n + ":\n    about: x\n    audit: |\n      touch ../run." + n + "; sleep 0.3; ls ../ | grep -c '^run\\.' > ../saw." + n + "; rm ../run." + n + "\n"
	}
	cfg, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := Build(cfg, "audit", ResolveAll(), false, "")
	base := t.TempDir()
	root := filepath.Join(base, "r")
	_ = os.Mkdir(root, 0o755)
	var prog bytes.Buffer
	for _, r := range Run(context.Background(), p, Options{Root: root, Shell: shell, Parallel: 1, Progress: &prog}) {
		if r.Status != Passed {
			t.Fatalf("%s %s", r.Step.Name, r.Status)
		}
	}
	for _, n := range []string{"a", "b", "c"} {
		b, _ := os.ReadFile(filepath.Join(base, "saw."+n))
		if strings.TrimSpace(string(b)) != "1" {
			t.Fatalf("%s saw %q running with --parallel 1", n, b)
		}
	}
	if !strings.Contains(prog.String(), "a          started\n") || !strings.Contains(prog.String(), "a          passed in ") {
		t.Fatalf("progress = %q", prog.String())
	}
}

// Source: README "Run": a failure shows its findings; a command reliab
// cannot start is a failure with the reason, not a pass; the last run's
// output is removed first so it is never read as this run's.
func TestRunErrorPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "u.xml"), []byte(`<testsuite><testcase name="old"><failure>stale</failure></testcase></testsuite>`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := Parse([]byte("version: 1\ncommands:\n  u: { about: x, audit: 'true', output: { file: u.xml, parse: junit } }\n"))
	p, _ := Build(cfg, "audit", ResolveAll(), false, "")
	shell, err := verify.FindShell()
	if err != nil {
		t.Skip(err)
	}
	r := Run(context.Background(), p, Options{Root: root, Shell: shell})[0]
	if r.Status != Failed || len(r.Findings) != 1 || !strings.Contains(r.Findings[0].Message, "not produced") {
		t.Fatalf("stale output read as this run's: %+v", r)
	}

	r = Run(context.Background(), p, Options{Root: root, Shell: filepath.Join(root, "no-such-shell")})[0]
	if r.Status != Failed || r.ExitCode != -1 || len(r.Findings) != 1 || !strings.HasPrefix(r.Findings[0].Message, "cannot run: ") {
		t.Fatalf("missing shell: %+v", r)
	}
	if r.Findings[0].Status != "open" || r.Findings[0].Command != "u" {
		t.Fatalf("finding = %+v", r.Findings[0])
	}

	// A log directory that cannot be created: a file sits where it goes.
	bad := t.TempDir()
	_ = os.WriteFile(filepath.Join(bad, ".reliab"), nil, 0o644)
	r = Run(context.Background(), p, Options{Root: bad, Shell: shell})[0]
	if r.Status != Failed || r.ExitCode != -1 || !strings.HasPrefix(r.Findings[0].Message, "cannot create log: ") {
		t.Fatalf("log error: %+v", r)
	}
}

// Source: profile-v2.md "Plan and schedule" and README "Run": a timeout is
// its own outcome, exit -1, with a finding saying so; cancelling the run
// cancels its steps.
func TestRunTimeoutAndCancel(t *testing.T) {
	shell, err := verify.FindShell()
	if err != nil {
		t.Skip(err)
	}
	cfg, _ := Parse([]byte("version: 1\ncommands:\n  s: { about: x, audit: 'sleep 10', timeout: 200ms }\n"))
	p, _ := Build(cfg, "audit", ResolveAll(), false, "")
	r := Run(context.Background(), p, Options{Root: t.TempDir(), Shell: shell})[0]
	if r.Status != TimedOut || r.ExitCode != -1 || len(r.Findings) != 1 || r.Findings[0].Message != "timed out after 200ms" {
		t.Fatalf("timeout: %+v", r)
	}

	cfg, _ = Parse([]byte("version: 1\ncommands:\n  s: { about: x, audit: 'sleep 10' }\n"))
	p, _ = Build(cfg, "audit", ResolveAll(), false, "")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r = Run(ctx, p, Options{Root: t.TempDir(), Shell: shell})[0]
	if r.Status != Cancelled || len(r.Findings) != 0 {
		t.Fatalf("cancel: %+v", r)
	}
}

// Source: README "Run": a failure shows its findings and their location;
// findings at lines outside the target are counted, not shown.
func TestLocationAndTail(t *testing.T) {
	for loc, want := range map[string]struct {
		f    string
		line int
		ok   bool
	}{
		"a/b.go:12:3": {"a/b.go", 12, true}, "a/b.go:7": {"a/b.go", 7, true},
		"a/b.go": {"a/b.go", 0, true}, "": {"", 0, false},
	} {
		f, l, ok := location(loc)
		if f != want.f || l != want.line || ok != want.ok {
			t.Errorf("location(%q) = %q %d %v", loc, f, l, ok)
		}
	}
	p := filepath.Join(t.TempDir(), "log")
	_ = os.WriteFile(p, []byte("1\n2\n3\n4\n"), 0o644)
	if got := tailFile(p, 2); got != "3\n4" {
		t.Errorf("tail 2 = %q", got)
	}
	if got := tailFile(p, 4); got != "1\n2\n3\n4" {
		t.Errorf("tail 4 = %q", got)
	}
	if got := tailFile(p+"x", 4); got != "" {
		t.Errorf("missing file tail = %q", got)
	}
}
