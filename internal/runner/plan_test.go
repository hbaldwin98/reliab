package runner

import (
	"bytes"
	"strings"
	"testing"
)

// Source for every test in this file: knowledge/decisions/profile-v2.md,
// "Contexts", "Targets" and "Plan and schedule".

const planYAML = `version: 1
commands:
  build:
    about: Compile.
    change: make
    pr: make
    audit: make
  unit:
    about: Tests.
    change: test {projects}
    pr: test all
    audit: test all
    targeted: test-cover {projects}
    after: [build]
  mutation:
    about: Mutants.
    pr: mutate changed
    audit: mutate all
    targeted: mutate {files}
    after: [unit]
    exclusive: true
  e2e:
    about: End to end.
    audit: e2e {tests}
`

func mustBuild(t *testing.T, ctx string, tg *Target, explicit bool) *Plan {
	t.Helper()
	cfg, err := Parse([]byte(planYAML))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(cfg, ctx, tg, explicit, "main")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func steps(p *Plan) map[string]*Step {
	m := map[string]*Step{}
	for _, s := range p.Steps {
		m[s.Name] = s
	}
	return m
}

func someFiles() *Target {
	return &Target{Specs: []string{"x"}, Files: []string{"src/a b.go", "src/c.go"}, Projects: []string{"./src"}}
}

func TestBuildContextPicksCommands(t *testing.T) {
	p := mustBuild(t, "change", someFiles(), false)
	s := steps(p)
	if len(s) != 2 || s["build"] == nil || s["unit"] == nil {
		t.Fatalf("change steps = %v", s)
	}
	if s["unit"].Line != "test ./src" || s["unit"].Targeted {
		t.Fatalf("unit line = %q targeted=%v; the context line with {projects} filled", s["unit"].Line, s["unit"].Targeted)
	}
	if strings.Join(p.NotHere, ",") != "e2e,mutation" {
		t.Fatalf("NotHere = %v", p.NotHere)
	}
}

func TestBuildStagesAndAfter(t *testing.T) {
	s := steps(mustBuild(t, "pr", someFiles(), false))
	if s["build"].Stage != 1 || s["unit"].Stage != 2 || s["mutation"].Stage != 3 {
		t.Fatalf("stages: %d %d %d", s["build"].Stage, s["unit"].Stage, s["mutation"].Stage)
	}
	if !s["mutation"].Exclusive || strings.Join(s["mutation"].After, ",") != "unit" {
		t.Fatalf("mutation = %+v", s["mutation"])
	}
}

func TestBuildTargetedLineReplacesContextLine(t *testing.T) {
	s := steps(mustBuild(t, "pr", someFiles(), true))
	if s["unit"].Line != "test-cover ./src" || !s["unit"].Targeted {
		t.Fatalf("unit = %q", s["unit"].Line)
	}
	if s["mutation"].Line != "mutate 'src/a b.go' src/c.go" {
		t.Fatalf("mutation = %q: each file one quoted sh word", s["mutation"].Line)
	}
	if s["build"].Line != "make" || s["build"].Targeted {
		t.Fatalf("build has no targeted line, its context line runs: %q", s["build"].Line)
	}
}

func TestBuildEmptyPlaceholderSkips(t *testing.T) {
	// No {tests}: running "e2e" with nothing would run everything.
	tg := someFiles()
	p := mustBuild(t, "audit", tg, true)
	if steps(p)["e2e"] != nil || strings.Join(p.NoTests, ",") != "e2e" {
		t.Fatalf("steps=%v NoTests=%v", steps(p), p.NoTests)
	}
}

func TestBuildPlaceholderOnEverythingIsAnError(t *testing.T) {
	cfg, _ := Parse([]byte(planYAML))
	_, err := Build(cfg, "audit", ResolveAll(), false, "")
	if err == nil || !strings.Contains(err.Error(), "commands.e2e.audit uses a placeholder") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildDropsAfterOutsidePlan(t *testing.T) {
	y := "version: 1\ncommands:\n  a: { about: x, pr: a }\n  b: { about: x, change: b, after: [a] }\n"
	cfg, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(cfg, "change", someFiles(), false, "")
	if err != nil {
		t.Fatal(err)
	}
	if b := steps(p)["b"]; b == nil || len(b.After) != 0 || b.Stage != 1 {
		t.Fatalf("b = %+v: a does not run in change, so b cannot wait on it", b)
	}
}

func TestBuildUnknownContext(t *testing.T) {
	cfg, _ := Parse([]byte(planYAML))
	if _, err := Build(cfg, "nightly", someFiles(), false, ""); err == nil {
		t.Fatal("want an error for a context that is not change, pr or audit")
	}
}

func TestPrintShowsFilledLinesAndAbout(t *testing.T) {
	var b bytes.Buffer
	mustBuild(t, "pr", someFiles(), true).Print(&b)
	out := b.String()
	for _, want := range []string{"reliab run pr", "$ test-cover ./src", "Mutants.", "runs alone", "after unit", "not in pr: e2e"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan missing %q:\n%s", want, out)
		}
	}
}
