package gate

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/verify"
)

// Every rule tested here is stated in knowledge/decisions/mutation-threshold.md.

const held = `protected_paths: ["mutation-baseline.json"]
mutation_baseline: mutation-baseline.json
commands:
  build: "true"
  mutation_changed:
    run: cp test/m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
    threshold: 80
tiers:
  prepr: { run: [build, mutation_changed] }
`

// counts writes the findings file the mutation command copies: killed
// mutants and one survivor each, all in src/a/a.go.
func counts(t *testing.T, root string, killed, survived int) {
	t.Helper()
	var fs []string
	for i := range survived {
		fs = append(fs, `{"test":"src/a/a.go: m`+string(rune('a'+i))+`","location":"src/a/a.go:`+strconv.Itoa(i+1)+`:1"}`)
	}
	c := `"killed":` + strconv.Itoa(killed) + `,"survived":` + strconv.Itoa(survived)
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{`+c+`},"mutantFiles":{"src/a/a.go":{`+c+`}},"findings":[`+strings.Join(fs, ",")+`]}`)
}

func prepr(t *testing.T, root string) *report.Report {
	t.Helper()
	rep, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: profile.DefaultPath, Tier: "prepr"})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func gateLines(t *testing.T, root, tier string) []MutationLine {
	t.Helper()
	p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	lines, err := Mutation(root, p, tier)
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

func failed(lines []MutationLine) bool {
	for _, l := range lines {
		if l.Fail {
			return true
		}
	}
	return false
}

func TestVerifyAndGateBelowThreshold(t *testing.T) {
	root := testrepo.New(t, held)
	counts(t, root, 7, 3)
	rep := prepr(t, root)

	var short []report.Finding
	for _, f := range rep.Findings {
		if f.Kind == report.KindBelowThreshold {
			short = append(short, f)
		}
	}
	if len(short) != 1 || !strings.Contains(short[0].Test, "70.0% below the threshold 80%") || len(rep.Findings) != 4 {
		t.Fatalf("findings = %+v, want three survivors and one shortfall", rep.Findings)
	}
	if s := rep.Run.Commands[1].Score; s == nil || s.Killed != 7 || s.Counted != 10 || *s.Threshold != 80 {
		t.Errorf("score = %+v", s)
	}
	lines := gateLines(t, root, "prepr")
	if !failed(lines) || !strings.Contains(lines[0].Text, "70.0% (7 killed of 10 counted), short by 1") {
		t.Errorf("gate = %+v, want a failure short by 1", lines)
	}

	counts(t, root, 8, 2)
	testrepo.Git(t, root, "add", "-A")
	prepr(t, root)
	if lines := gateLines(t, root, "prepr"); failed(lines) || !strings.Contains(lines[0].Text, "met") {
		t.Errorf("at the threshold: gate = %+v", lines)
	}
}

func TestGateBaselineScope(t *testing.T) {
	root := testrepo.New(t, held)
	testrepo.Write(t, root, "mutation-baseline.json", `{"schemaVersion":"0.1.0","scopes":[{"path":"src/a/**","score":90}]}`)
	counts(t, root, 8, 2)
	rep := prepr(t, root)
	found := false
	for _, f := range rep.Findings {
		if f.Kind == report.KindBelowThreshold && strings.Contains(f.Test, "in src/a/** 80.0% below its baseline 90%") {
			found = true
		}
	}
	if !found {
		t.Errorf("no baseline shortfall in %+v", rep.Findings)
	}
	lines := gateLines(t, root, "prepr")
	if len(lines) != 2 || lines[0].Fail || !lines[1].Fail || !strings.Contains(lines[1].Text, "baseline src/a/** 90%") {
		t.Errorf("gate = %+v, want the threshold met and the baseline failed", lines)
	}
}

func TestGateFailsWhatItCannotScore(t *testing.T) {
	// No counts: a tool that measured nothing never passes the gate.
	root := testrepo.New(t, held)
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.3.0","findings":[]}`)
	prepr(t, root)
	if lines := gateLines(t, root, "prepr"); !failed(lines) || !strings.Contains(lines[0].Text, "no mutant counts") {
		t.Errorf("no counts: gate = %+v", lines)
	}

	// Stale: the tree changed after the report.
	counts(t, root, 9, 1)
	prepr(t, root)
	testrepo.Write(t, root, "src/a/a.txt", "edited")
	if lines := gateLines(t, root, "prepr"); !failed(lines) || !strings.Contains(lines[0].Text, "stale") {
		t.Errorf("stale: gate = %+v", lines)
	}

	// Skipped: the build failed, so mutation never ran.
	root = testrepo.New(t, strings.Replace(held, `build: "true"`, "build: exit 1", 1))
	counts(t, root, 9, 1)
	prepr(t, root)
	if lines := gateLines(t, root, "prepr"); !failed(lines) || !strings.Contains(lines[0].Text, "skipped (build-failed)") {
		t.Errorf("skipped: gate = %+v", lines)
	}

	// No report at all is a usage error, not a pass.
	p, _ := profile.Load(filepath.Join(root, profile.DefaultPath))
	if _, err := Mutation(root, p, "nightly"); err == nil {
		t.Error("no nightly report: no error")
	}
}

func TestGateIgnoresCommandsHeldToNothing(t *testing.T) {
	root := testrepo.New(t, strings.Replace(strings.Replace(held, "    threshold: 80\n", "", 1), "mutation_baseline: mutation-baseline.json\n", "", 1))
	counts(t, root, 1, 9)
	prepr(t, root)
	if lines := gateLines(t, root, "prepr"); len(lines) != 0 {
		t.Errorf("gate = %+v, want nothing to judge", lines)
	}
}

// A baseline with no scopes holds nothing, so a command with no threshold
// is not judged; a command the tier runs first is judged like any other
// (mutation-threshold.md).
func TestGateEmptyBaselineAndFirstCommand(t *testing.T) {
	src := strings.Replace(held, "    threshold: 80\n", "", 1)
	root := testrepo.New(t, strings.Replace(src, "run: [build, mutation_changed]", "run: [mutation_changed]", 1))
	testrepo.Write(t, root, "mutation-baseline.json", `{"schemaVersion":"0.1.0","scopes":[]}`)
	counts(t, root, 1, 9)
	testrepo.Git(t, root, "add", "-A")
	prepr(t, root)
	if lines := gateLines(t, root, "prepr"); len(lines) != 0 {
		t.Errorf("empty baseline: gate = %+v, want nothing to judge", lines)
	}
	testrepo.Write(t, root, "mutation-baseline.json", `{"schemaVersion":"0.1.0","scopes":[{"path":"src/a/**","score":50}]}`)
	prepr(t, root)
	if lines := gateLines(t, root, "prepr"); len(lines) != 1 || !lines[0].Fail || !strings.Contains(lines[0].Text, "10.0% (1 killed of 10 counted)") {
		t.Errorf("first command: gate = %+v", lines)
	}
}

// A scope the tool could not score fails the gate; a run that counted
// nothing has nothing to hold (mutation-threshold.md).
func TestGateUnmeasuredAndEmpty(t *testing.T) {
	root := testrepo.New(t, held)
	testrepo.Write(t, root, "mutation-baseline.json", `{"schemaVersion":"0.1.0","scopes":[{"path":"src/a/**","score":50}]}`)
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{"killed":9,"survived":1},"findings":[{"test":"src/a/a.go: m","location":"src/a/a.go:1:1"}]}`)
	prepr(t, root)
	lines := gateLines(t, root, "prepr")
	if len(lines) != 2 || !lines[1].Fail || lines[1].Text != "baseline src/a/** 50%: not scored, the tool reported no per-file counts" {
		t.Errorf("unmeasured: gate = %+v", lines)
	}
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{"killed":0,"survived":0},"findings":[]}`)
	prepr(t, root)
	lines = gateLines(t, root, "prepr")
	if failed(lines) || len(lines) != 1 || lines[0].Text != "threshold 80%: no mutants counted, nothing to hold" {
		t.Errorf("nothing counted: gate = %+v", lines)
	}
}

// R8: a skip is reported with its reason and detail.
func TestWhy(t *testing.T) {
	if got := why(report.Skip{Reason: report.SkipBudget, Detail: "killed after 3.0s"}); got != "budget: killed after 3.0s" {
		t.Errorf("why = %q", got)
	}
	if got := why(report.Skip{Reason: report.SkipBuildFailed}); got != "build-failed" {
		t.Errorf("why = %q", got)
	}
}

// A mutation command skipped as not relevant has no changed code to hold
// (knowledge/decisions/relevance-skips.md).
func TestGateNotRelevantPasses(t *testing.T) {
	root := testrepo.New(t, strings.Replace(held, "    threshold: 80\n", "    threshold: 80\n    relevant_when: [\"src/data/**\"]\n", 1))
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	counts(t, root, 1, 9)
	testrepo.Write(t, root, "src/a/a.txt", "edited")
	prepr(t, root)
	lines := gateLines(t, root, "prepr")
	if len(lines) != 1 || lines[0].Fail || !strings.HasPrefix(lines[0].Text, "skipped as not relevant (the change since main touches only") {
		t.Errorf("gate = %+v", lines)
	}
}

// A command the tier runs and the profile holds to a threshold, that the
// report neither ran nor skipped (the profile changed after it), is not
// scored and fails (mutation-threshold.md).
func TestGateFailsACommandThatDidNotRun(t *testing.T) {
	root := testrepo.New(t, strings.Replace(held, "run: [build, mutation_changed]", "run: [build]", 1))
	counts(t, root, 9, 1)
	prepr(t, root)
	testrepo.Write(t, root, profile.DefaultPath, strings.Replace(readFile(t, root, profile.DefaultPath), "run: [build]", "run: [build, mutation_changed]", 1))
	lines := gateLines(t, root, "prepr")
	var found bool
	for _, l := range lines {
		if l.Command == "mutation_changed" && l.Fail && l.Text == "did not run: not scored" {
			found = true
		}
	}
	if !found {
		t.Errorf("gate = %+v, want mutation_changed failed as not run", lines)
	}
}

func readFile(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// report-freshness.md: a tree that changed while the run ran leaves a stale
// report even once the edit is undone. The gate then says the tree changed
// during the run, rather than naming the same tree twice as if it moved.
func TestGateStaleWhenTheTreeChangedDuringTheRun(t *testing.T) {
	root := testrepo.New(t, strings.Replace(held, "run: cp test/m.json .reliab/out/m.json", "run: cp test/m.json .reliab/out/m.json && echo mid > src/a/mid.txt", 1))
	counts(t, root, 9, 1)
	rep := prepr(t, root)
	if err := os.Remove(filepath.Join(root, "src/a/mid.txt")); err != nil {
		t.Fatal(err)
	}
	if rep.Run.TreeHashEnd == "" {
		t.Fatalf("the run did not record the tree changing: %+v", rep.Run)
	}
	lines := gateLines(t, root, "prepr")
	if !failed(lines) || !strings.Contains(lines[0].Text, "changed while it ran") || strings.Count(lines[0].Text, rep.Run.TreeHash) > 1 {
		t.Errorf("gate = %+v, want it to say the tree changed while the report ran", lines)
	}
}
