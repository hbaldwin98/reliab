package triage

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/verify"
)

// The shortfall's life, per knowledge/decisions/mutation-threshold.md: it is
// never classified, it routes to its survivors while they need the agent,
// then stops for a person, and a person confirming a survivor equivalent
// re-scores the report at once.
func TestShortfallFollowsItsSurvivors(t *testing.T) {
	root := testrepo.New(t, `commands:
  mutation_changed:
    run: cp test/m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
    threshold: 80
tiers:
  prepr: { run: [mutation_changed] }
`)
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{"killed":3,"survived":1},"findings":[{"test":"src/a/a.go: CONDITIONALS_NEGATION","location":"src/a/a.go:3:1"}]}`)
	rep, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %+v, want the survivor and its shortfall", rep.Findings)
	}
	surv, short := rep.Findings[0], rep.Findings[1]
	if short.Kind != report.KindBelowThreshold {
		t.Fatalf("second finding = %+v", short)
	}

	if _, err := Classify(root, short.ID, report.KindHarnessDefect, "r", time.Now()); err == nil || !strings.Contains(err.Error(), "never classified") {
		t.Errorf("classifying the shortfall: err = %v", err)
	}

	var out bytes.Buffer
	if _, err := Route(&out, root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1 still need you: "+surv.ID) || strings.Contains(out.String(), "STOP: every survivor") {
		t.Errorf("route while the survivor is unclassified:\n%s", out.String())
	}

	if _, err := Classify(root, surv.ID, report.KindEquivalentMutant, "the negated branch is unreachable", time.Now()); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	_, _ = Route(&out, root)
	if !strings.Contains(out.String(), "STOP: every survivor counted against this score is with a person") {
		t.Errorf("route once the survivor is with a person:\n%s", out.String())
	}
	r, _ := report.Read(report.LatestPath(root, "prepr"))
	if s := r.Run.Commands[0].Score; s.PendingEquivalent != 1 || s.Counted != 4 {
		t.Errorf("after classify: score %+v, want 1 pending equivalent, still counted", s)
	}

	// A person confirms it: the mutant leaves the count, 3 of 3 meets 80%.
	if _, err := Acknowledge(root, surv.ID, "human:t", time.Now()); err != nil {
		t.Fatal(err)
	}
	r, _ = report.Read(report.LatestPath(root, "prepr"))
	if s := r.Run.Commands[0].Score; s.Counted != 3 || s.Equivalent != 1 {
		t.Errorf("after acknowledgement: score %+v", s)
	}
	for _, f := range r.Findings {
		if f.Kind == report.KindBelowThreshold {
			t.Errorf("shortfall still there after the score met the threshold: %+v", f)
		}
	}
	// knowledge/decisions/report-storage.md: the history copy is re-scored
	// the same way, so it is the same record.
	h, err := report.Read(report.HistoryPath(root, r))
	if err != nil {
		t.Fatal(err)
	}
	if s := h.Run.Commands[0].Score; s.Counted != 3 || s.Equivalent != 1 || len(h.Findings) != len(r.Findings) {
		t.Errorf("history after acknowledgement: score %+v, %d findings, want the latest report's", s, len(h.Findings))
	}
}

// knowledge/decisions/report-storage.md: classifying a survivor that only
// history holds re-scores that history copy as the latest report would be.
func TestClassifyFixedSurvivorRescoresHistory(t *testing.T) {
	root := testrepo.New(t, `commands:
  mutation_changed:
    run: cp test/m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
    threshold: 80
tiers:
  prepr: { run: [mutation_changed] }
`)
	run := func(m int, findings string) *report.Report {
		t.Helper()
		testrepo.Write(t, root, "test/m.json", findings)
		rep, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr",
			Now: func() time.Time { return time.Now().Add(time.Duration(m) * time.Minute) }})
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	first := run(0, `{"schemaVersion":"0.4.0","mutants":{"killed":3,"survived":1},"findings":[{"test":"src/a/a.go: CONDITIONALS_NEGATION","location":"src/a/a.go:3:1"}]}`)
	run(1, `{"schemaVersion":"0.4.0","mutants":{"killed":4,"survived":0},"findings":[]}`)
	if _, err := Classify(root, first.Findings[0].ID, report.KindEquivalentMutant, "the negated branch is unreachable", time.Now()); err != nil {
		t.Fatal(err)
	}
	h, err := report.Read(report.HistoryPath(root, first))
	if err != nil {
		t.Fatal(err)
	}
	if s := h.Run.Commands[0].Score; s == nil || s.PendingEquivalent != 1 {
		t.Errorf("history score after classify = %+v, want 1 pending equivalent", s)
	}

	// The history copy says so too when it could not be re-scored
	// (knowledge/decisions/mutation-threshold.md).
	testrepo.Write(t, root, "reliability-profile.yaml", "not: [a profile")
	tiers, err := Classify(root, first.Findings[0].ID, report.KindRequirementSilent, "is the branch meant to be reachable?", time.Now())
	var stale *NotRescored
	if !errors.As(err, &stale) || len(tiers) != 1 || !strings.Contains(err.Error(), "history: ") {
		t.Errorf("tiers %v, err %v; want the history change recorded and a NotRescored naming history", tiers, err)
	}
}

// R8: route says which commands were skipped as not relevant, and why each
// command with relevance rules ran (knowledge/decisions/relevance-skips.md).
func TestRouteRelevance(t *testing.T) {
	root := testrepo.New(t, `commands:
  faults_db: { run: "true", relevant_when: ["src/data/**"] }
  api_fuzz: { run: "true", relevant_when: ["src/a/**"] }
tiers:
  inner: { run: [faults_db, api_fuzz] }
`)
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, "src/a/a.txt", "edited")
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := Route(&out, root); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"skipped faults_db: not relevant: the change since main touches only src/a/a.txt; it is relevant only to src/data/**; report it in the PR summary (R8)",
		"ran api_fuzz: src/a/a.txt changed since main and matches src/a/**",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("route lacks %q:\n%s", want, out.String())
		}
	}
}

const scoped = `protected_paths: ["mutation-baseline.json"]
mutation_baseline: mutation-baseline.json
commands:
  mutation_changed:
    run: cp test/m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
tiers:
  prepr: { run: [mutation_changed] }
`

// Two survivors, one in src/a and one in src/b; src/a holds 1 killed of 2.
const twoAreas = `{"schemaVersion":"0.4.0","mutants":{"killed":3,"survived":2},
"mutantFiles":{"src/a/x.go":{"killed":1,"survived":1},"src/b/y.go":{"killed":2,"survived":1}},
"findings":[{"test":"src/a/x.go: m","location":"src/a/x.go:1:1"},{"test":"src/b/y.go: m","location":"src/b/y.go:1:1"}]}`

func prepr(t *testing.T, root string) *report.Report {
	t.Helper()
	rep, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func shortfall(t *testing.T, r *report.Report) report.Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Kind == report.KindBelowThreshold {
			return f
		}
	}
	t.Fatalf("no shortfall in %+v", r.Findings)
	return report.Finding{}
}

func idOf(t *testing.T, r *report.Report, test string) string {
	t.Helper()
	for _, f := range r.Findings {
		if f.Test == test {
			return f.ID
		}
	}
	t.Fatalf("no finding %q", test)
	return ""
}

// A baseline scope's shortfall routes the agent to the survivors in that
// scope only: they are the ones it counts (mutation-threshold.md).
func TestRouteShortfallNamesItsScopesSurvivors(t *testing.T) {
	root := testrepo.New(t, scoped)
	testrepo.Write(t, root, "mutation-baseline.json", `{"schemaVersion":"0.1.0","scopes":[{"path":"src/a/**","score":90}]}`)
	testrepo.Write(t, root, "test/m.json", twoAreas)
	rep := prepr(t, root)
	inA, inB := idOf(t, rep, "src/a/x.go: m"), idOf(t, rep, "src/b/y.go: m")
	var out bytes.Buffer
	if _, err := Route(&out, root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1 still need you: "+inA+")") || strings.Contains(out.String(), "need you: "+inB) || strings.Contains(out.String(), ", "+inB+")") {
		t.Errorf("route names survivors outside src/a/** (%s in, %s out):\n%s", inA, inB, out.String())
	}
}

// When a report's scores cannot be recomputed, classify and acknowledge
// still record the change and say the scores were left as they were;
// never silently (mutation-threshold.md).
func TestClassifySaysWhenScoresWereNotRecomputed(t *testing.T) {
	root := testrepo.New(t, scoped)
	testrepo.Write(t, root, "test/m.json", twoAreas)
	rep := prepr(t, root)
	id := idOf(t, rep, "src/a/x.go: m")
	testrepo.Write(t, root, "mutation-baseline.json", `{"schemaVersion":"0.1.0","scopes":[{"path":"src/a/**"}]}`)

	tiers, err := Classify(root, id, report.KindEquivalentMutant, "unreachable", time.Now())
	var stale *NotRescored
	if !errors.As(err, &stale) || len(tiers) != 1 || !strings.Contains(err.Error(), "mutation-baseline.json") || !strings.Contains(err.Error(), "not recomputed") {
		t.Fatalf("tiers %v, err %v; want the change recorded and a NotRescored naming the baseline", tiers, err)
	}
	if f := findingByID(t, root, id); f.Kind != report.KindEquivalentMutant {
		t.Errorf("classification not recorded: %+v", f)
	}
	if _, err := Acknowledge(root, id, "human:t", time.Now()); !errors.As(err, &stale) {
		t.Errorf("acknowledge: err = %v, want NotRescored", err)
	}

	// A report with no mutation counts has nothing to recompute.
	plain := failingRepo(t)
	testrepo.Write(t, plain, "reliability-profile.yaml", "not: [a profile")
	if _, err := Classify(plain, "F-analyzers", report.KindHarnessDefect, "r", time.Now()); err != nil {
		t.Errorf("no mutation counts: err = %v", err)
	}
}

func findingByID(t *testing.T, root, id string) report.Finding {
	t.Helper()
	r, err := report.Read(report.LatestPath(root, "prepr"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Findings {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no finding %s", id)
	return report.Finding{}
}

// Acknowledgements carry to the next verify run: a confirmed equivalent
// leaves the count there too, and a person's acceptance of a shortfall
// holds only while its numbers stay the same (mutation-threshold.md).
func TestAcknowledgementsCarryAcrossVerifyRuns(t *testing.T) {
	root := testrepo.New(t, strings.Replace(scoped, "    output: .reliab/out/m.json\n", "    output: .reliab/out/m.json\n    threshold: 80\n", 1))
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{"killed":3,"survived":1},"findings":[{"test":"src/a/x.go: m","location":"src/a/x.go:1:1"}]}`)
	rep := prepr(t, root)
	surv := idOf(t, rep, "src/a/x.go: m")
	if _, err := Classify(root, surv, report.KindEquivalentMutant, "unreachable", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Acknowledge(root, surv, "human:t", time.Now()); err != nil {
		t.Fatal(err)
	}
	rep = prepr(t, root)
	if s := rep.Run.Commands[0].Score; s.Counted != 3 || s.Equivalent != 1 {
		t.Errorf("next run: score %+v, want the confirmed equivalent left out", s)
	}
	for _, f := range rep.Findings {
		if f.Kind == report.KindBelowThreshold {
			t.Errorf("next run: shortfall %+v, want 3 of 3 met", f)
		}
	}

	// A shortfall a person accepted stays accepted while its numbers hold.
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{"killed":7,"survived":3},"findings":[{"test":"src/a/x.go: m","location":"src/a/x.go:1:1"},{"test":"src/a/x.go: n","location":"src/a/x.go:2:1"},{"test":"src/a/x.go: o","location":"src/a/x.go:3:1"}]}`)
	short := shortfall(t, prepr(t, root))
	if _, err := Acknowledge(root, short.ID, "human:t", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := findingByID(t, root, short.ID); got.Status != report.StatusAcknowledged {
		t.Fatalf("after acknowledging: %+v", got)
	}
	if got := shortfall(t, prepr(t, root)); got.ID != short.ID || got.Status != report.StatusAcknowledged {
		t.Errorf("same numbers next run: %+v, want still acknowledged", got)
	}
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{"killed":6,"survived":4},"findings":[{"test":"src/a/x.go: m","location":"src/a/x.go:1:1"}]}`)
	if got := shortfall(t, prepr(t, root)); got.ID == short.ID || got.Status != report.StatusOpen {
		t.Errorf("new numbers next run: %+v, want a new open shortfall", got)
	}
}
