package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/verify"
)

// Source for every test in this file: knowledge/decisions/profile-v2.md,
// "Plan and schedule" (parallel unless after or exclusive; a failed after
// blocks) and "Targets" (findings outside the target are dropped).

func runYAML(t *testing.T, y string, tg *Target) map[string]*StepResult {
	t.Helper()
	shell, err := verify.FindShell()
	if err != nil {
		t.Skip(err)
	}
	cfg, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if tg == nil {
		tg = ResolveAll()
	}
	p, err := Build(cfg, "audit", tg, false, "")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	out := map[string]*StepResult{}
	for _, r := range Run(context.Background(), p, Options{Root: root, Shell: shell, Parallel: 4}) {
		out[r.Step.Name] = r
	}
	return out
}

func TestRunBlocksAfterAFailure(t *testing.T) {
	r := runYAML(t, `version: 1
commands:
  build: { about: x, audit: "exit 3" }
  unit: { about: x, audit: "echo ran > unit.txt", after: [build] }
  lint: { about: x, audit: "true" }
`, nil)
	if r["build"].Status != Failed || r["build"].ExitCode != 3 {
		t.Fatalf("build = %+v", r["build"])
	}
	if r["unit"].Status != Blocked || r["unit"].BlockedBy != "build" {
		t.Fatalf("unit = %+v", r["unit"])
	}
	if r["lint"].Status != Passed {
		t.Fatalf("lint = %+v: independent of build, it still runs", r["lint"])
	}
}

func TestRunBlockCascades(t *testing.T) {
	r := runYAML(t, `version: 1
commands:
  a: { about: x, audit: "false" }
  b: { about: x, audit: "true", after: [a] }
  c: { about: x, audit: "true", after: [b] }
`, nil)
	if r["b"].Status != Blocked || r["c"].Status != Blocked || r["c"].BlockedBy != "b" {
		t.Fatalf("b=%+v c=%+v", r["b"], r["c"])
	}
}

func TestRunExclusiveRunsAlone(t *testing.T) {
	// Each step marks itself running in a shared directory, sleeps, and
	// records what else was running. The exclusive step must see nothing,
	// and nothing may see it.
	step := func(n string) string {
		return `touch ../run.` + n + `; sleep 0.4; ls ../ | grep '^run\.' | tr '\n' ' ' > ../saw.` + n + `; rm ../run.` + n
	}
	y := "version: 1\ncommands:\n"
	for _, n := range []string{"a", "b", "c", "x"} {
		y += "  " + n + ":\n    about: x\n    audit: |\n      " + step(n) + "\n"
		if n == "x" {
			y += "    exclusive: true\n"
		}
	}
	shell, err := verify.FindShell()
	if err != nil {
		t.Skip(err)
	}
	cfg, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := Build(cfg, "audit", ResolveAll(), false, "")
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, r := range Run(context.Background(), p, Options{Root: root, Shell: shell, Parallel: 4}) {
		if r.Status != Passed {
			t.Fatalf("%s: %s", r.Step.Name, r.Status)
		}
	}
	saw := func(n string) string {
		b, _ := os.ReadFile(filepath.Join(base, "saw."+n))
		return strings.TrimSpace(string(b))
	}
	if got := saw("x"); got != "run.x" {
		t.Fatalf("exclusive x saw %q running", got)
	}
	for _, n := range []string{"a", "b", "c"} {
		if strings.Contains(saw(n), "run.x") {
			t.Fatalf("%s ran alongside exclusive x: %q", n, saw(n))
		}
	}
	if !strings.Contains(saw("a")+saw("b")+saw("c"), " ") {
		t.Fatalf("a, b and c never overlapped: %q %q %q; non-exclusive steps run in parallel", saw("a"), saw("b"), saw("c"))
	}
}

func TestRunTimeout(t *testing.T) {
	r := runYAML(t, `version: 1
commands:
  slow: { about: x, audit: "sleep 10", timeout: 300ms }
  next: { about: x, audit: "true", after: [slow] }
`, nil)
	if r["slow"].Status != TimedOut || r["slow"].Elapsed > 8*time.Second {
		t.Fatalf("slow = %+v", r["slow"])
	}
	if r["next"].Status != Blocked {
		t.Fatalf("next = %+v", r["next"])
	}
}

func TestRunEnvAndLog(t *testing.T) {
	shell, err := verify.FindShell()
	if err != nil {
		t.Skip(err)
	}
	cfg, _ := Parse([]byte("version: 1\ncommands:\n  e: { about: x, pr: 'echo \"$RELIAB_CONTEXT|$RELIAB_BASE|$RELIAB_TARGET_PROJECTS\"' }\n"))
	tg := &Target{Specs: []string{"x"}, Files: []string{"a.go"}, Projects: []string{"./a", "./b"}}
	p, _ := Build(cfg, "pr", tg, false, "origin/main")
	root := t.TempDir()
	res := Run(context.Background(), p, Options{Root: root, Shell: shell})
	log, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(res[0].Log)))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(log)); got != "pr|origin/main|./a\n./b" {
		t.Fatalf("log = %q", got)
	}
}

func TestRunDropsFindingsOutsideTarget(t *testing.T) {
	junit := `<testsuites><testsuite name="s"><testcase name="T1" classname="c" file="src/in.go" line="5"><failure message="m">in</failure></testcase><testcase name="T2" classname="c" file="src/out.go" line="5"><failure message="m">out</failure></testcase></testsuite></testsuites>`
	tg := &Target{Specs: []string{"src/in.go"}, Files: []string{"src/in.go"}, Ranges: map[string][][2]int{}}
	y := "version: 1\ncommands:\n  u:\n    about: x\n    audit: |\n      printf '%s' '" + junit + "' > u.xml; exit 1\n    output: { file: u.xml, parse: junit }\n"
	r := runYAML(t, y, tg)["u"]
	if r.Status != Failed {
		t.Fatalf("u = %+v", r)
	}
	var locs []string
	for _, f := range r.Findings {
		locs = append(locs, f.Test+"@"+f.Location)
	}
	if len(r.Findings) == 0 || strings.Contains(strings.Join(locs, " "), "out.go") {
		t.Fatalf("findings = %v (outside=%d); want only src/in.go's", locs, r.Outside)
	}
}

// Source: README "reliab.yaml": output is "a file (or glob)", e.g.
// **/TestResults/*.trx when a tool writes one file per project.
func TestOutputFilesGlobCrossesDirectories(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"a/TestResults/1.trx", "b/c/TestResults/2.trx", "a/TestResults/x.log"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := outputFiles(root, "**/TestResults/*.trx")
	if len(got) != 2 {
		t.Fatalf("got %v, want both .trx files", got)
	}
}

func TestTargetContains(t *testing.T) {
	tg := &Target{Files: []string{"a.go", "b.go"}, Tests: []string{"a_test.ts"}, Ranges: map[string][][2]int{"a.go": {{10, 20}}}}
	cases := []struct {
		f    string
		line int
		want bool
	}{
		{"a.go", 10, true}, {"a.go", 20, true}, {"a.go", 9, false}, {"a.go", 21, false},
		{"a.go", 0, true}, {"b.go", 999, true}, {"c.go", 1, false}, {"a_test.ts", 3, true},
	}
	for _, c := range cases {
		if got := tg.Contains(c.f, c.line); got != c.want {
			t.Errorf("Contains(%s, %d) = %v", c.f, c.line, got)
		}
	}
	if !ResolveAll().Contains("anything", 1) {
		t.Error("everything contains everything")
	}
}
