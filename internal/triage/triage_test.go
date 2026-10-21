package triage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/verify"
)

// failingRepo returns a repo whose inner, prepr, and on-demand reports each
// hold the open finding F-analyzers.
func failingRepo(t *testing.T) string {
	t.Helper()
	root := testrepo.New(t, "commands:\n  analyzers: echo lint broke; exit 1\n")
	for _, opt := range []verify.Options{{Tier: "inner"}, {Tier: "prepr"}, {Tier: "inner", Only: []string{"analyzers"}}} {
		opt.Root, opt.Profile = root, "reliability-profile.yaml"
		if _, _, err := verify.Run(context.Background(), opt); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func finding(t *testing.T, root, tier string) report.Finding {
	t.Helper()
	r, err := report.Read(report.LatestPath(root, tier))
	if err != nil {
		t.Fatal(err)
	}
	return r.Findings[0]
}

func TestClassifyUpdatesEveryTier(t *testing.T) {
	root := failingRepo(t)
	tiers, err := Classify(root, "F-analyzers", report.KindHarnessDefect, "lint config is wrong", time.Now())
	if err != nil || len(tiers) != len(Tiers) {
		t.Fatalf("tiers=%v err=%v", tiers, err)
	}
	for _, tier := range Tiers {
		if f := finding(t, root, tier); f.Kind != report.KindHarnessDefect || f.Classification.Reason != "lint config is wrong" {
			t.Errorf("%s: %+v", tier, f)
		}
	}
}

func TestClassifyRejections(t *testing.T) {
	root := failingRepo(t)
	if _, err := Classify(root, "F-analyzers", "bug", "r", time.Now()); err == nil {
		t.Error("unknown kind accepted")
	}
	if _, err := Classify(root, "F-analyzers", report.KindHarnessDefect, " ", time.Now()); err == nil {
		t.Error("empty reason accepted")
	}
	if _, err := Classify(root, "F-missing", report.KindHarnessDefect, "r", time.Now()); err == nil {
		t.Error("unknown finding accepted")
	}
}

func TestAcknowledge(t *testing.T) {
	root := failingRepo(t)
	if _, err := Acknowledge(root, "F-analyzers", "human:me", time.Now()); err == nil {
		t.Fatal("acknowledged an agent-kind finding")
	}
	if _, err := Classify(root, "F-analyzers", report.KindLegacyDefectOrQuirk, "old lint debt", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Acknowledge(root, "F-analyzers", "human:me", time.Now()); err != nil {
		t.Fatal(err)
	}
	if f := finding(t, root, "prepr"); f.Status != report.StatusAcknowledged || f.Acknowledged.By != "human:me" {
		t.Fatalf("finding = %+v", f)
	}
	if _, err := Classify(root, "F-analyzers", report.KindNewCodeDefect, "r", time.Now()); err == nil {
		t.Error("reclassified an acknowledged finding")
	}
}

func TestRoute(t *testing.T) {
	var buf bytes.Buffer
	if n, err := Route(&buf, testrepo.New(t, "commands:\n  build: \"true\"\n")); err != nil || n != 0 || !strings.Contains(buf.String(), "no report yet") {
		t.Fatalf("no report: n=%d err=%v out=%q", n, err, buf.String())
	}

	root := failingRepo(t)
	buf.Reset()
	n, err := Route(&buf, root)
	out := buf.String()
	if err != nil || n != 1 || !strings.Contains(out, "reliab findings classify F-analyzers") || !strings.Contains(out, "lint broke") {
		t.Fatalf("n=%d err=%v out=%q", n, err, out)
	}
	// plugin/skills/verify/SKILL.md: route "lists each open finding and what
	// to do"; it must not also claim there are none.
	if strings.Contains(out, "no open findings") {
		t.Errorf("open finding listed as none: %q", out)
	}
	if _, err := Classify(root, "F-analyzers", report.KindCharacterizationDiff, "snapshot moved", time.Now()); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if _, err := Route(&buf, root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "STOP") {
		t.Fatalf("human kind not routed to a stop: %q", buf.String())
	}
}

func TestRouteListsBudgetSkips(t *testing.T) {
	// plugin/skills/verify/SKILL.md "Exit codes": "reliab report route lists
	// budget skips" (R8).
	root := testrepo.New(t, `commands:
  build: sleep 10
tiers:
  inner: { budget_seconds: 1 }
`)
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Route(&buf, root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "skipped build: budget") {
		t.Errorf("budget skip not listed: %q", buf.String())
	}
}

func TestRouteShowsShortMessageInFull(t *testing.T) {
	// internal/triage/triage.go mutantAction doc: a mutant's diff is "shown in
	// full up to mutantLines"; knowledge/decisions/external-adapters.md: route
	// "shows enough of the diff to see the mutated line". A diff of exactly
	// that many lines is not truncated.
	root := testrepo.New(t, `commands:
  mutation_changed:
    run: cp m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
`)
	lines := make([]string, mutantLines)
	for i := range lines {
		lines[i] = "line" + string(rune('a'+i))
	}
	msg, _ := json.Marshal(strings.Join(lines, "\n"))
	m := `{"schemaVersion":"0.1.0","findings":[{"test":"a.go: mutant","message":` + string(msg) + `}]}`
	testrepo.Write(t, root, "m.json", m)
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Route(&buf, root); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, lines[mutantLines-1]) || strings.Contains(out, "\n  ...") {
		t.Errorf("%d-line diff not shown in full: %q", mutantLines, out)
	}
}

func TestRouteSurvivingMutant(t *testing.T) {
	root := testrepo.New(t, `commands:
  mutation_changed:
    run: cp m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
`)
	diff := `--- a.go
+++ replace < with <=
@@ -1,13 +1,13 @@
 1
 2
 3
 4
 5
 6
-if a < b {
+if a <= b {
 7
`
	msg, _ := json.Marshal(diff)
	m := `{"schemaVersion":"0.1.0","findings":[{"test":"a.go: replace < with <=","message":` + string(msg) + `}]}`
	if err := os.WriteFile(filepath.Join(root, "m.json"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if n, err := Route(&buf, root); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v out=%q", n, err, buf.String())
	}
	out := buf.String()
	if strings.Contains(out, "seed") {
		t.Errorf("mutant routed like a test failure: %q", out)
	}
	// Three outcomes: a test citing stated intent, a question when nothing
	// states it, or equivalent.
	for _, want := range []string{"write a test that cites it and fails with this mutation", "--kind requirement-silent", "--kind equivalent-mutant"} {
		if !strings.Contains(out, want) {
			t.Errorf("route advice lacks %q: %q", want, out)
		}
	}
	if strings.Contains(out, "%[1]s") || strings.Contains(out, "%!") {
		t.Errorf("finding id not filled in: %q", out)
	}
	if !strings.Contains(out, "+if a <= b {") {
		t.Errorf("mutated line not shown: %q", out)
	}
}

func TestEquivalentMutant(t *testing.T) {
	// Only a surviving mutant can be equivalent.
	root := failingRepo(t)
	if _, err := Classify(root, "F-analyzers", report.KindEquivalentMutant, "cannot change behavior", time.Now()); err == nil || !strings.Contains(err.Error(), "not a mutation command") {
		t.Fatalf("analyzers finding classified equivalent: %v", err)
	}

	root = testrepo.New(t, `commands:
  mutation_changed:
    run: cp m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
`)
	m := `{"schemaVersion":"0.1.0","findings":[{"test":"a.go: replace > with >= in clamp","location":"a.go:7:9"}]}`
	if err := os.WriteFile(filepath.Join(root, "m.json"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
		t.Fatal(err)
	}
	id := finding(t, root, "prepr").ID
	var buf bytes.Buffer
	if _, err := Route(&buf, root); err != nil || !strings.Contains(buf.String(), "reliab findings classify "+id+" --kind equivalent-mutant") {
		t.Fatalf("route does not offer equivalent-mutant: %v %q", err, buf.String())
	}
	// plugin/skills/mutation/SKILL.md step 1: the agent reads "the location,
	// the operator" of each survivor from route.
	if out := buf.String(); !strings.Contains(out, "a.go: replace > with >= in clamp") || !strings.Contains(out, "at a.go:7:9") {
		t.Errorf("mutant operator or location not shown: %q", out)
	}
	if _, err := Classify(root, id, report.KindEquivalentMutant, "clamp returns max either way at the boundary", time.Now()); err != nil {
		t.Fatal(err)
	}
	f := finding(t, root, "prepr")
	if !report.NeedsHuman(f) || report.NeedsAgent(f) {
		t.Fatalf("equivalent mutant should wait for a human, not the agent: %+v", f)
	}
	buf.Reset()
	if _, err := Route(&buf, root); err != nil || !strings.Contains(buf.String(), "STOP: a human confirms it is equivalent") {
		t.Fatalf("route: %v %q", err, buf.String())
	}
	if _, err := Acknowledge(root, id, "human:reviewer", time.Now()); err != nil {
		t.Fatal(err)
	}
	if f := finding(t, root, "prepr"); f.Status != report.StatusAcknowledged {
		t.Fatalf("not acknowledged: %+v", f)
	}
}

func TestRequirementSilent(t *testing.T) {
	root := failingRepo(t)
	if _, err := Classify(root, "F-analyzers", report.KindRequirementSilent, "unspecified", time.Now()); err == nil || !strings.Contains(err.Error(), "not a mutation command") {
		t.Fatalf("analyzers finding classified requirement-silent: %v", err)
	}

	root = testrepo.New(t, `commands:
  mutation_changed:
    run: cp m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
`)
	m := `{"schemaVersion":"0.1.0","findings":[{"test":"a.go: replace == with != in name_or_command","location":"a.go:12:5"}]}`
	if err := os.WriteFile(filepath.Join(root, "m.json"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
		t.Fatal(err)
	}
	id := finding(t, root, "prepr").ID
	if _, err := Classify(root, id, report.KindRequirementSilent, "should a finding without a test name show its command?", time.Now()); err != nil {
		t.Fatal(err)
	}
	f := finding(t, root, "prepr")
	if !report.NeedsHuman(f) || report.NeedsAgent(f) {
		t.Fatalf("a silent requirement should wait for a human, not the agent: %+v", f)
	}
	var buf bytes.Buffer
	if _, err := Route(&buf, root); err != nil || !strings.Contains(buf.String(), "STOP: a human says whether this behavior is intended") {
		t.Fatalf("route: %v %q", err, buf.String())
	}
}

// Any command can fail because of the machine; the kind waits for a human.
func TestEnvironment(t *testing.T) {
	root := failingRepo(t)
	if _, err := Classify(root, "F-analyzers", report.KindEnvironment, "re-ran: too many consoles in use, max consoles is 128", time.Now()); err != nil {
		t.Fatal(err)
	}
	f := finding(t, root, "inner")
	if !report.NeedsHuman(f) || report.NeedsAgent(f) {
		t.Fatalf("an environment failure should wait for a human: %+v", f)
	}
	var buf bytes.Buffer
	if _, err := Route(&buf, root); err != nil || !strings.Contains(buf.String(), "STOP: the machine failed, not the code") {
		t.Fatalf("route: %v %q", err, buf.String())
	}
	if _, err := Acknowledge(root, "F-analyzers", "human:reviewer", time.Now()); err != nil {
		t.Fatal(err)
	}
}

// knowledge/decisions/snapshot-diff-labels.md: route shows a finding's diff
// label, up to 24 lines of its diff, and, while it is unclassified, the
// snapshot advice; findings without a label route as before.
func TestRouteSnapshotDiffs(t *testing.T) {
	root := testrepo.New(t, `commands:
  snapshots:
    run: cp s.json .reliab/out/s.json
    format: findings
    output: .reliab/out/s.json
`)
	var diff []string
	for i := 1; i <= snapshotLines; i++ {
		diff = append(diff, fmt.Sprintf("+line %d", i))
	}
	msg, _ := json.Marshal(strings.Join(diff, "\n"))
	s := `{"schemaVersion":"0.2.0","findings":[` +
		`{"test":"snapshots/added.snap","diff":"additive-only","message":` + string(msg) + `},` +
		`{"test":"snapshots/changed.snap","diff":"values-changed","message":"-a\n+b"},` +
		`{"test":"plain","message":"panicked"}]}`
	if err := os.WriteFile(filepath.Join(root, "s.json"), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
		t.Fatal(err)
	}
	route := func() map[string]string {
		var buf bytes.Buffer
		if _, err := Route(&buf, root); err != nil {
			t.Fatal(err)
		}
		// One block per finding, keyed by its subject.
		blocks := map[string]string{}
		for _, b := range strings.Split(buf.String(), "\n\n")[1:] {
			head, _, _ := strings.Cut(b, "\n")
			fields := strings.Fields(head)
			blocks[fields[len(fields)-1]] = b
		}
		return blocks
	}
	blocks := route()
	added, changed, plain := blocks["snapshots/added.snap"], blocks["snapshots/changed.snap"], blocks["plain"]
	if !strings.Contains(added, "diff: additive only") || !strings.Contains(changed, "diff: values changed or removed") {
		t.Errorf("labels not shown:\n%s\n%s", added, changed)
	}
	for _, b := range []string{added, changed} {
		if !strings.Contains(b, "--kind characterization-diff") || !strings.Contains(b, "--kind new-code-defect") {
			t.Errorf("snapshot advice missing:\n%s", b)
		}
	}
	if !strings.Contains(added, diff[snapshotLines-1]) || strings.Contains(added, "\n  ...") {
		t.Errorf("%d-line diff not shown in full:\n%s", snapshotLines, added)
	}
	if strings.Contains(plain, "diff:") || strings.Contains(plain, "characterization-diff") || !strings.Contains(plain, "seed") {
		t.Errorf("unlabelled finding routed as a snapshot:\n%s", plain)
	}

	// Once classified, the kind's own advice applies; the label still shows.
	id := ""
	r, _ := report.Read(report.LatestPath(root, "prepr"))
	for _, f := range r.Findings {
		if f.Test == "snapshots/changed.snap" {
			id = f.ID
		}
	}
	if _, err := Classify(root, id, report.KindCharacterizationDiff, "the pane's final state was renamed on purpose", time.Now()); err != nil {
		t.Fatal(err)
	}
	changed = route()["snapshots/changed.snap"]
	if !strings.Contains(changed, "STOP: a human approves or reverts the snapshot") || strings.Contains(changed, "--kind new-code-defect") || !strings.Contains(changed, "diff: values changed") {
		t.Errorf("classified snapshot routed wrongly:\n%s", changed)
	}
}

// knowledge/decisions/reviewer-verdict.md: the reviewer judged the
// classifications as they stood, so reclassifying voids its verdict; a
// person's acknowledgement does not.
func TestClassifyClearsVerdictAckKeepsIt(t *testing.T) {
	root := failingRepo(t)
	setVerdict := func() {
		r, err := report.Read(report.LatestPath(root, "prepr"))
		if err != nil {
			t.Fatal(err)
		}
		r.Verdict = &report.Verdict{Outcome: report.VerdictPass, By: "reliab:reviewer", At: report.Now(time.Now()), TreeHash: r.Run.TreeHash}
		if err := report.Rewrite(root, r); err != nil {
			t.Fatal(err)
		}
	}
	verdict := func() *report.Verdict {
		r, err := report.Read(report.LatestPath(root, "prepr"))
		if err != nil {
			t.Fatal(err)
		}
		return r.Verdict
	}
	setVerdict()
	if _, err := Classify(root, "F-analyzers", report.KindLegacyDefectOrQuirk, "lint debt from before this change", time.Now()); err != nil {
		t.Fatal(err)
	}
	if v := verdict(); v != nil {
		t.Fatalf("verdict survived a reclassification: %+v", v)
	}
	setVerdict()
	if _, err := Acknowledge(root, "F-analyzers", "human:owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if v := verdict(); v == nil {
		t.Fatal("a person's acknowledgement cleared the verdict")
	}
}

// fixableRepo returns a repo whose analyzers command fails until src/fixed
// exists, with one inner and one prepr run at the given minute.
func fixableRepo(t *testing.T) string {
	t.Helper()
	root := testrepo.New(t, "commands:\n  analyzers: test -f src/fixed || { echo lint broke; exit 1; }\n")
	run(t, root, 0)
	return root
}

// run runs inner and prepr on a clock m minutes ahead, so each run has its
// own history file. The clock still moves: budgets read it.
func run(t *testing.T, root string, m int) {
	t.Helper()
	offset := time.Duration(m) * time.Minute
	for _, tier := range []string{"inner", "prepr"} {
		opt := verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: tier, Now: func() time.Time { return time.Now().Add(offset) }}
		if _, _, err := verify.Run(context.Background(), opt); err != nil {
			t.Fatal(err)
		}
	}
}

// historyFinding is id as the history copy of tier's latest report holds it.
func historyFinding(t *testing.T, root, tier, id string) report.Finding {
	t.Helper()
	latest, err := report.Read(report.LatestPath(root, tier))
	if err != nil {
		t.Fatal(err)
	}
	return findingIn(t, report.HistoryPath(root, latest), id)
}

func findingIn(t *testing.T, path, id string) report.Finding {
	t.Helper()
	r, err := report.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Findings {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("%s has no %s", path, id)
	return report.Finding{}
}

// knowledge/decisions/report-storage.md: history is the durable record. A
// classification and a person's acknowledgement reach the run's history
// copy, so they survive the next run, which replaces the latest report and
// no longer has a fixed finding (the PR summary's fixed list reads them).
func TestClassifyAndAckReachHistory(t *testing.T) {
	root := fixableRepo(t)
	if _, err := Classify(root, "F-analyzers", report.KindLegacyDefectOrQuirk, "old lint debt", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Acknowledge(root, "F-analyzers", "human:me", time.Now()); err != nil {
		t.Fatal(err)
	}
	firstPrepr, err := report.Read(report.LatestPath(root, "prepr"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tier := range []string{"inner", "prepr"} {
		f := historyFinding(t, root, tier, "F-analyzers")
		if f.Kind != report.KindLegacyDefectOrQuirk || f.Classification == nil || f.Classification.Reason != "old lint debt" ||
			f.Status != report.StatusAcknowledged || f.Acknowledged.By != "human:me" {
			t.Errorf("%s history: %+v", tier, f)
		}
	}

	testrepo.Write(t, root, "src/fixed", "")
	run(t, root, 1)
	if f := findingIn(t, report.HistoryPath(root, firstPrepr), "F-analyzers"); f.Classification == nil || f.Classification.Reason != "old lint debt" {
		t.Errorf("classification lost from history after a re-run: %+v", f)
	}
}

// AGENTS.md: classify every failure before reporting (R4). A finding fixed
// before it was classified is in no latest report; classify still records
// its kind on the newest run that saw it and clears the pre-PR verdict.
func TestClassifyAFixedFinding(t *testing.T) {
	root := fixableRepo(t)
	run(t, root, 1)
	fixedIn, err := report.Read(report.LatestPath(root, "prepr"))
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Write(t, root, "src/fixed", "")
	run(t, root, 2)
	latest, err := report.Read(report.LatestPath(root, "prepr"))
	if err != nil {
		t.Fatal(err)
	}
	latest.Verdict = &report.Verdict{Outcome: report.VerdictPass, By: "reliab:reviewer", At: report.Now(time.Now()), TreeHash: latest.Run.TreeHash}
	if err := report.Rewrite(root, latest); err != nil {
		t.Fatal(err)
	}

	got, err := Classify(root, "F-analyzers", report.KindHarnessDefect, "lint config named a missing file", time.Now())
	if err != nil || len(got) != 1 || !strings.Contains(got[0], "history prepr "+fixedIn.Run.StartedAt) {
		t.Fatalf("got %v, %v; want the newest run that saw it, prepr at %s", got, err, fixedIn.Run.StartedAt)
	}
	if f := findingIn(t, report.HistoryPath(root, fixedIn), "F-analyzers"); f.Kind != report.KindHarnessDefect || f.Classification.Reason != "lint config named a missing file" {
		t.Errorf("history finding = %+v", f)
	}
	if after, err := report.Read(report.LatestPath(root, "prepr")); err != nil || after.Verdict != nil || len(after.Findings) != 0 {
		t.Errorf("latest prepr after classify: %+v, %v; want the verdict cleared and nothing else", after, err)
	}
	if _, err := Classify(root, "F-never", report.KindHarnessDefect, "r", time.Now()); err == nil || !strings.Contains(err.Error(), "nor in history") {
		t.Errorf("unknown finding: %v", err)
	}
}

// The same, when the only run that saw the finding is the oldest in history.
func TestClassifyAFindingOnlyTheFirstRunSaw(t *testing.T) {
	root := testrepo.New(t, "commands:\n  analyzers: test -f src/fixed || { echo lint broke; exit 1; }\n")
	at := func(m int) func() time.Time {
		return func() time.Time { return time.Now().Add(time.Duration(m) * time.Minute) }
	}
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner", Now: at(0)}); err != nil {
		t.Fatal(err)
	}
	testrepo.Write(t, root, "src/fixed", "")
	run(t, root, 1)
	if got, err := Classify(root, "F-analyzers", report.KindHarnessDefect, "lint config named a missing file", time.Now()); err != nil || len(got) != 1 || !strings.HasPrefix(got[0], "history inner ") {
		t.Fatalf("got %v, %v", got, err)
	}
}

// knowledge/decisions/finding-classification.md: a property or fuzz failure
// whose cited source says nothing about the generated input is a question
// for a human (requirement-silent), not a legacy quirk; route says so for
// those commands, and equivalent-mutant stays for mutants only.
func TestRequirementSilentForGeneratedInputs(t *testing.T) {
	requirementSilentFor(t, generatedCommands, "the source the property cites")
}

// knowledge/decisions/finding-classification.md: a fault test's failure,
// where nothing states what should happen under the injected fault, is a
// question for a human too. Found onboarding a repository whose fault
// findings could only be filed as legacy quirks.
func TestRequirementSilentForFaults(t *testing.T) {
	requirementSilentFor(t, faultCommands, "the same injected fault")
}

func requirementSilentFor(t *testing.T, ids []string, advice string) {
	t.Helper()
	for _, id := range ids {
		root := testrepo.New(t, "commands:\n  "+id+":\n    run: cp test/p.xml .reliab/out/p.xml; exit 1\n    format: junit\n    output: .reliab/out/p.xml\ntiers:\n  prepr: { run: ["+id+"] }\n")
		testrepo.Write(t, root, "test/p.xml", `<testsuite><testcase name="TestOrderQuantity"><failure>quantity 0 accepted</failure></testcase></testsuite>`)
		if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
			t.Fatal(err)
		}
		f := finding(t, root, "prepr")
		var buf bytes.Buffer
		if _, err := Route(&buf, root); err != nil || !strings.Contains(buf.String(), "reliab findings classify "+f.ID+" --kind requirement-silent") ||
			!strings.Contains(buf.String(), advice) {
			t.Errorf("%s: route = %q, %v", id, buf.String(), err)
		}
		if _, err := Classify(root, f.ID, report.KindEquivalentMutant, "r", time.Now()); err == nil {
			t.Errorf("%s: a %s finding classified equivalent-mutant", id, id)
		}
		if _, err := Classify(root, f.ID, report.KindRequirementSilent, "the spec states no minimum quantity: is 0 allowed?", time.Now()); err != nil {
			t.Errorf("%s: %v", id, err)
		}
		if f := finding(t, root, "prepr"); !report.NeedsHuman(f) {
			t.Errorf("%s: requirement-silent does not wait for a human: %+v", id, f)
		}
	}
}

// knowledge/decisions/named-tests.md: route prints the tests a run named
// and each command that waits for tests to be named.
func TestRouteNamedTestsAndNeedsInput(t *testing.T) {
	root := testrepo.New(t, "commands:\n  store_tests:\n    run: \"true\"\n    needs: [tests]\n  mutation_changed:\n    run: \"true\"\n    needs: [mutation_tests]\n"+
		"tiers:\n  inner: { run: [store_tests, mutation_changed] }\n")
	opt := verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner", MutationTests: []string{"Ns.A", "Ns.B"}}
	if _, _, err := verify.Run(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Route(&buf, root); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"(complete=false)", "mutation tests named: Ns.A, Ns.B", "incomplete store_tests: no tests named: re-run with --tests"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("route lacks %q:\n%s", want, buf.String())
		}
	}
	if strings.Contains(buf.String(), "incomplete mutation_changed") {
		t.Errorf("route lists a command whose tests were named:\n%s", buf.String())
	}

	// It prints only what the run named.
	opt.Tests, opt.MutationTests = []string{"--test A"}, nil
	if _, _, err := verify.Run(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if _, err := Route(&buf, root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\ntests named: --test A\n") || strings.Contains(buf.String(), "\nmutation tests named:") {
		t.Errorf("tests named, mutation tests not:\n%s", buf.String())
	}
}

// knowledge/decisions/finding-classification.md: classify records a fixed
// finding's kind on the newest run of this branch that holds it, the run
// the PR summary reads; .reliab/ is shared by every branch of a checkout,
// so a later run on another branch must not take it.
func TestClassifyAFixedFindingOnThisBranch(t *testing.T) {
	root := testrepo.New(t, "commands:\n  analyzers: test -f src/fixed || { echo lint broke; exit 1; }\n")
	commit := func(msg string) {
		t.Helper()
		testrepo.Git(t, root, "add", "-A")
		testrepo.Git(t, root, "commit", "-qm", msg)
	}
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, "src/feature.txt", "feature work")
	commit("feature")
	run(t, root, 1)
	featureRun, err := report.Read(report.LatestPath(root, "prepr"))
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Git(t, root, "checkout", "-q", "-b", "other", "main")
	testrepo.Write(t, root, "src/other.txt", "other work")
	commit("other")
	run(t, root, 2)
	testrepo.Git(t, root, "checkout", "-q", "feature")
	testrepo.Write(t, root, "src/fixed", "")
	commit("fix")
	run(t, root, 3)

	got, err := Classify(root, "F-analyzers", report.KindHarnessDefect, "lint config named a missing file", time.Now())
	if err != nil || len(got) != 1 || !strings.Contains(got[0], "history prepr "+featureRun.Run.StartedAt) {
		t.Fatalf("got %v, %v; want this branch's prepr at %s", got, err, featureRun.Run.StartedAt)
	}
}

// AGENTS.md: run the inner tier when a change is done and classify every
// failure before reporting; a failure the inner tier saw and the next inner
// run no longer has is classified with no pre-PR report yet.
func TestClassifyAFixedFindingBeforeAnyPrepr(t *testing.T) {
	root := testrepo.New(t, "commands:\n  analyzers: test -f src/fixed || { echo lint broke; exit 1; }\n")
	inner := func(m int) {
		t.Helper()
		at := func() time.Time { return time.Now().Add(time.Duration(m) * time.Minute) }
		if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner", Now: at}); err != nil {
			t.Fatal(err)
		}
	}
	inner(0)
	testrepo.Write(t, root, "src/fixed", "")
	inner(1)
	if got, err := Classify(root, "F-analyzers", report.KindHarnessDefect, "lint config named a missing file", time.Now()); err != nil || len(got) != 1 || !strings.HasPrefix(got[0], "history inner ") {
		t.Fatalf("got %v, %v; want the first inner run", got, err)
	}
}
