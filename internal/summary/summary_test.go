package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/gate"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/triage"
	"github.com/hbaldwin98/reliab/internal/verify"
)

const fixture = `commands:
  build: "true"
  unit:
    run: cp test/u.xml .reliab/out/u.xml; exit 1
    format: junit
    output: .reliab/out/u.xml
  mutation_changed:
    run: cp test/m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
  mutation_full:
    run: cp test/mf.json .reliab/out/mf.json
    format: findings
    output: .reliab/out/mf.json
  coverage: "true"
tiers:
  inner: { run: [build, unit] }
  prepr: { run: [build, unit, mutation_changed, corpus, api_fuzz] }
`

const (
	twoFailures = `<testsuite><testcase name="TestRetryBackoff"><failure>want 2s</failure></testcase><testcase name="TestParseHeader"><failure>panic</failure></testcase></testsuite>`
	oneFailure  = `<testsuite><testcase name="TestRetryBackoff"/><testcase name="TestParseHeader"><failure>panic</failure></testcase></testsuite>`
	survivors   = `{"schemaVersion":"0.3.0","mutants":{"killed":17,"survived":2,"notCovered":1},"findings":[
  {"test":"src/retry/retry.go: CONDITIONALS_BOUNDARY","location":"src/retry/retry.go:41:9","message":"surviving mutant"},
  {"test":"src/retry/retry.go: INCREMENT_DECREMENT","location":"src/retry/retry.go:57:3","message":"surviving mutant"}]}`
	fullSurvivor = `{"schemaVersion":"0.3.0","findings":[{"test":"src/cache/cache.go: ARITHMETIC_BASE","location":"src/cache/cache.go:12:4"}]}`
)

// change builds the kind of report step 3's acceptance names
// (knowledge/decisions/pr-evidence-summary.md): an inner run with two
// failures, one then fixed; an on-demand mutation run; and a prepr run
// with skips, a classified survivor, and an open human-kind finding.
func change(t *testing.T) string {
	t.Helper()
	root := testrepo.New(t, fixture)
	testrepo.Write(t, root, "test/u.xml", twoFailures)
	testrepo.Write(t, root, "test/m.json", survivors)
	testrepo.Write(t, root, "test/mf.json", fullSurvivor)
	// In the future: verify sets its budget deadline from Now.
	start := time.Date(2036, 10, 18, 9, 0, 0, 0, time.UTC)
	step := 0
	run := func(opt verify.Options) *report.Report {
		t.Helper()
		step++
		at := start.Add(time.Duration(step) * time.Minute)
		opt.Root, opt.Profile, opt.Now = root, "reliability-profile.yaml", func() time.Time { return at }
		rep, _, err := verify.Run(context.Background(), opt)
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	run(verify.Options{Tier: "inner"})
	run(verify.Options{Tier: "prepr", Only: []string{"mutation_full"}})
	testrepo.Write(t, root, "test/u.xml", oneFailure)
	rep := run(verify.Options{Tier: "prepr"})

	byTest := map[string]string{}
	for _, f := range rep.Findings {
		byTest[f.Test] = f.ID
	}
	classify := func(test, kind, reason string) {
		t.Helper()
		if _, err := triage.Classify(root, byTest[test], kind, reason, start); err != nil {
			t.Fatal(err)
		}
	}
	classify("TestParseHeader", report.KindNewCodeDefect, "header parser panics on an empty value")
	classify("src/retry/retry.go: CONDITIONALS_BOUNDARY", report.KindRequirementSilent, "should a retry at exactly the max delay be allowed?")
	classify("src/retry/retry.go: INCREMENT_DECREMENT", report.KindEquivalentMutant, "the counter is only logged")
	if _, err := triage.Acknowledge(root, byTest["src/retry/retry.go: INCREMENT_DECREMENT"], "human:owner", start); err != nil {
		t.Fatal(err)
	}
	return root
}

func build(t *testing.T, root string) *Summary {
	t.Helper()
	s, err := Build(Options{Root: root, Tier: "prepr", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ids(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Subject)
	}
	return out
}

// knowledge/decisions/pr-evidence-summary.md: open, acknowledged, fixed, and
// not re-checked findings; questions from open human-kind findings only;
// mutation counts; skips with their reasons; commands outside the tier.
func TestBuild(t *testing.T) {
	root := change(t)
	s := build(t, root)
	if !s.Fresh || !s.Complete || s.Tier != "prepr" || s.Head == "" {
		t.Errorf("header = %+v", s)
	}
	if got := strings.Join(ids(s.Open), ","); got != "TestParseHeader,src/retry/retry.go: CONDITIONALS_BOUNDARY" {
		t.Errorf("open = %s", got)
	}
	if got := strings.Join(ids(s.Acknowledged), ","); got != "src/retry/retry.go: INCREMENT_DECREMENT" {
		t.Errorf("acknowledged = %s", got)
	}
	if got := strings.Join(ids(s.Fixed), ","); got != "TestRetryBackoff" {
		t.Errorf("fixed = %s, want the failure the inner run had and prepr no longer does", got)
	}
	if got := strings.Join(ids(s.NotRechecked), ","); got != "src/cache/cache.go: ARITHMETIC_BASE" {
		t.Errorf("not rechecked = %s, want the on-demand mutation_full survivor", got)
	}
	// Only the open human-kind finding is a question; the acknowledged one
	// is answered, and new-code-defect is the agent's.
	if len(s.Questions) != 1 || s.Questions[0].Kind != report.KindRequirementSilent || s.Questions[0].Reason == "" || s.Questions[0].Ask == "" {
		t.Errorf("questions = %+v", s.Questions)
	}
	if len(s.Mutation) != 1 || s.Mutation[0].Mutants == nil || *s.Mutation[0].Mutants != (report.MutantCounts{Killed: 17, Survived: 2, NotCovered: 1}) || len(s.Mutation[0].Survivors) != 2 {
		t.Errorf("mutation = %+v", s.Mutation)
	}
	skips := map[string]string{}
	for _, k := range s.Skipped {
		skips[k.ID] = k.Reason
	}
	if skips["corpus"] != report.SkipNoCommand || skips["api_fuzz"] != report.SkipNotApplicable {
		t.Errorf("skipped = %+v", s.Skipped)
	}
	if strings.Join(s.NotInTier, ",") != "coverage,mutation_full" {
		t.Errorf("not in tier = %v", s.NotInTier)
	}
}

// Every line of the Markdown is a fact a person can find in the report.
func TestMarkdown(t *testing.T) {
	root := change(t)
	md := Markdown(build(t, root))
	for _, want := range []string{
		"### Evidence: reliab prepr report",
		"- Run: prepr tier, profile `test`",
		"complete.",
		"- Reviewer verdict: none recorded.",
		"| unit | 1 | 1 |",
		"| corpus | no command in the profile |",
		"| api_fuzz | role not applicable | T7: none |",
		"Not in the prepr tier: coverage, mutation_full.",
		"- `mutation_changed`: 19 tested, 17 killed, 2 survived; 1 not covered by any test.",
		"| requirement-silent | open | should a retry at exactly the max delay be allowed? |",
		"| equivalent-mutant | acknowledged by human:owner | the counter is only logged |",
		"- Open: 2. Acknowledged by a person: 1. Fixed during the change: 1.",
		"| inner 2036-10-18T09:01:00Z |",
		"not re-checked",
		"#### Questions for a human",
		"1. `F-mutation-changed-",
		"[requirement-silent] src/retry/retry.go: CONDITIONALS_BOUNDARY at src/retry/retry.go:41:9: say whether this behavior is intended. Recorded reason: \"should a retry at exactly the max delay be allowed?\"",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Stale") || strings.Contains(md, "Incomplete") {
		t.Errorf("fresh, complete report flagged:\n%s", md)
	}

	testrepo.Write(t, root, "src/a/a.txt", "edited after the run")
	if md := Markdown(build(t, root)); !strings.HasPrefix(md, "### Evidence: reliab prepr report\n\n> **Stale:**") {
		t.Errorf("stale report not flagged at the top:\n%s", md)
	}
}

// knowledge/decisions/pr-evidence-summary.md: the run lines give the
// reviewer verdict and whether it is for this report's tree.
func TestVerdictLine(t *testing.T) {
	root := change(t)
	rep, err := report.Read(report.LatestPath(root, "prepr"))
	if err != nil {
		t.Fatal(err)
	}
	rep.Verdict = &report.Verdict{Outcome: report.VerdictPass, By: "reliab:reviewer", At: "2026-10-18T10:00:00Z", Notes: "re-ran prepr", TreeHash: rep.Run.TreeHash}
	if err := report.Rewrite(root, rep); err != nil {
		t.Fatal(err)
	}
	if md := Markdown(build(t, root)); !strings.Contains(md, "- Reviewer verdict: **pass** by reliab:reviewer at 2026-10-18T10:00:00Z, for this report's tree. Notes: re-ran prepr") {
		t.Errorf("verdict line:\n%s", md)
	}
	rep.Verdict.TreeHash = strings.Repeat("0", 40)
	if err := report.Rewrite(root, rep); err != nil {
		t.Fatal(err)
	}
	if md := Markdown(build(t, root)); !strings.Contains(md, "**not for this report's tree**") {
		t.Errorf("verdict for another tree not flagged:\n%s", md)
	}
}

// knowledge/decisions/pr-evidence-summary.md: an incomplete report is flagged
// at the top, budget skips are listed, and a base with no merge base says the
// earlier runs were not read.
func TestIncompleteAndNoBase(t *testing.T) {
	root := testrepo.New(t, "commands:\n  build: sleep 5\n  analyzers: \"true\"\ntiers:\n  prepr: { budget_seconds: 1, run: [build, analyzers] }\n")
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
		t.Fatal(err)
	}
	s, err := Build(Options{Root: root, Tier: "prepr", Base: "no-such-ref"})
	if err != nil {
		t.Fatal(err)
	}
	md := Markdown(s)
	for _, want := range []string{"> **Incomplete:**", "| build | budget ran out | killed after", "| analyzers | budget ran out |", "earlier runs not read: no merge base with no-such-ref",
		// knowledge/decisions/pr-evidence-summary.md: Questions say "None"
		// only when the protected paths were checked.
		"- Protected paths not checked: no merge base with no-such-ref"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}

	testrepo.Write(t, root, "reliability-profile.yaml", "not: [a profile")
	if s, err := Build(Options{Root: root, Tier: "prepr", Base: "main"}); err != nil || s.ProtectedError != "the profile could not be read" || len(s.Protected) != 0 {
		t.Errorf("unreadable profile: %+v, %v", s, err)
	}
}

// knowledge/decisions/pr-evidence-summary.md: --format json gives the same
// facts as one object; Markdown and JSON are the only formats.
func TestWrite(t *testing.T) {
	root := change(t)
	s := build(t, root)
	var b bytes.Buffer
	if err := Write(&b, s, "json"); err != nil {
		t.Fatal(err)
	}
	var back Summary
	if err := json.Unmarshal(b.Bytes(), &back); err != nil || len(back.Fixed) != 1 || back.TreeHash != s.TreeHash {
		t.Fatalf("json round trip: %v %+v", err, back)
	}
	if err := Write(&b, s, "html"); err == nil {
		t.Error("unknown format accepted")
	}
	if _, err := Build(Options{Root: testrepo.New(t, "commands:\n  build: \"true\"\n"), Tier: "prepr"}); err == nil {
		t.Error("no report: want an error")
	}
}

// Step 3's acceptance: a finding classified and then fixed keeps its kind
// and reason in the fixed list, because classify writes the run's history
// copy (knowledge/decisions/report-storage.md); and a protected file the
// branch changed is a question for a human until a person approves it
// (knowledge/decisions/protected-path-approval.md).
func TestFixedClassificationAndProtectedQuestion(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\"snapshots/**\"]\ncommands:\n  unit: test -f src/fixed || { echo want 2s; exit 1; }\ntiers:\n  prepr: { run: [unit] }\n")
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	commit := func() {
		t.Helper()
		testrepo.Git(t, root, "add", "-A")
		testrepo.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "change")
	}
	start := time.Date(2036, 10, 18, 9, 0, 0, 0, time.UTC)
	prepr := func(m int) {
		t.Helper()
		at := start.Add(time.Duration(m) * time.Minute)
		opt := verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr", Now: func() time.Time { return at }}
		if _, _, err := verify.Run(context.Background(), opt); err != nil {
			t.Fatal(err)
		}
	}
	testrepo.Write(t, root, "snapshots/seed.txt", "the failing case")
	commit()
	prepr(1)
	if _, err := triage.Classify(root, "F-unit", report.KindNewCodeDefect, "retry waits 1s, the ticket says 2s", start); err != nil {
		t.Fatal(err)
	}
	testrepo.Write(t, root, "src/fixed", "")
	commit()
	prepr(2)

	s := build(t, root)
	if len(s.Fixed) != 1 || s.Fixed[0].Kind != report.KindNewCodeDefect || s.Fixed[0].Reason != "retry waits 1s, the ticket says 2s" {
		t.Errorf("fixed = %+v, want F-unit with its classification", s.Fixed)
	}
	md := Markdown(s)
	for _, want := range []string{
		"| `F-unit` | new-code-defect | command unit |",
		"1. `snapshots/seed.txt` (protected by `snapshots/**`) changed since `main` without a person's approval",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}

	p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Approve(root, p, "main", []string{"snapshots/seed.txt"}, "human:owner", start); err != nil {
		t.Fatal(err)
	}
	md = Markdown(build(t, root))
	if !strings.Contains(md, "None: no open finding or protected-path change waits for a person.") ||
		!strings.Contains(md, "- `snapshots/seed.txt` (protected by `snapshots/**`): approved by human:owner") {
		t.Errorf("approved seed:\n%s", md)
	}
}

// knowledge/decisions/pr-evidence-summary.md: a fixed finding shows the kind
// and reason it was classified with; finding-classification.md: classify
// records the kind of a finding fixed before it was classified, so the PR
// summary has it. .reliab/ is shared by every branch of a checkout, so a
// later run of the same finding on another branch must not take the
// classification away from the run this branch's summary reads.
func TestClassifyAfterFixReachesSummaryAcrossBranches(t *testing.T) {
	root := testrepo.New(t, "commands:\n  unit: test -f src/fixed || { echo want 2s; exit 1; }\ntiers:\n  prepr: { run: [unit] }\n")
	commit := func(msg string) {
		t.Helper()
		testrepo.Git(t, root, "add", "-A")
		testrepo.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", msg)
	}
	start := time.Date(2036, 10, 18, 9, 0, 0, 0, time.UTC)
	prepr := func(m int) {
		t.Helper()
		at := start.Add(time.Duration(m) * time.Minute)
		opt := verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr", Now: func() time.Time { return at }}
		if _, _, err := verify.Run(context.Background(), opt); err != nil {
			t.Fatal(err)
		}
	}
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, "src/feature.txt", "feature work")
	commit("feature")
	prepr(1)
	// Another branch in the same checkout runs later and sees the same id.
	testrepo.Git(t, root, "checkout", "-q", "-b", "other", "main")
	testrepo.Write(t, root, "src/other.txt", "other work")
	commit("other")
	prepr(2)
	testrepo.Git(t, root, "checkout", "-q", "feature")
	testrepo.Write(t, root, "src/fixed", "")
	commit("fix")
	prepr(3)

	if _, err := triage.Classify(root, "F-unit", report.KindNewCodeDefect, "retry waits 1s, the ticket says 2s", start); err != nil {
		t.Fatal(err)
	}
	s := build(t, root)
	if len(s.Fixed) != 1 || s.Fixed[0].Kind != report.KindNewCodeDefect || s.Fixed[0].Reason != "retry waits 1s, the ticket says 2s" {
		t.Errorf("fixed = %+v, want F-unit with its classification", s.Fixed)
	}
	if md := Markdown(s); !strings.Contains(md, "| `F-unit` | new-code-defect | command unit |") {
		t.Errorf("markdown lacks the classified fixed finding:\n%s", md)
	}
}

// knowledge/decisions/protected-path-approval.md: the profile guards its own
// threshold, relevance rules, and baseline, so a branch that edits it, even
// to drop its own protection, has a question for a human.
func TestProfileChangeIsAQuestion(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\"reliability-profile.yaml\"]\ncommands:\n  build: \"true\"\ntiers:\n  prepr: { run: [build] }\n")
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, "reliability-profile.yaml", "schemaVersion: 0.1.0\nname: test\nstarting_condition: greenfield\n"+
		"paths: { product: [\"src/**\"], tests: [\"test/**\"], ignore: [\"docs/**\"] }\n"+testrepo.Roles+
		"commands:\n  build: \"true\"\ntiers:\n  prepr: { run: [build] }\n")
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
		t.Fatal(err)
	}
	md := Markdown(build(t, root))
	if want := "1. `reliability-profile.yaml` (protected by `reliability-profile.yaml`) changed since `main` without a person's approval"; !strings.Contains(md, want) {
		t.Errorf("markdown lacks %q:\n%s", want, md)
	}
}

// knowledge/decisions/named-tests.md: the summary shows the tests a run
// named, and says a run is incomplete because a command waits for tests,
// not because of the budget.
func TestNamedTestsAndNeedsInput(t *testing.T) {
	root := testrepo.New(t, "commands:\n  build: \"true\"\n  mutation_changed:\n    run: \"true\"\n    needs: [mutation_tests]\n"+
		"tiers:\n  prepr: { run: [build, mutation_changed] }\n")
	opt := verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr", Tests: []string{"--test A", "--where 'cat==x,y'"}}
	if _, _, err := verify.Run(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	s, err := Build(Options{Root: root, Tier: "prepr", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	md := Markdown(s)
	for _, want := range []string{
		"- Tests named (`--tests`): `--test A`; `--where 'cat==x,y'`.",
		"> **Incomplete:** no tests were named for mutation_changed, which need them; see Skipped.",
		"| mutation_changed | no tests named (incomplete) | no mutation tests named: re-run with --mutation-tests |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "budget ran out") || strings.Contains(md, "Mutation tests named") {
		t.Errorf("markdown names a cause or a selection the run did not have:\n%s", md)
	}
	var buf bytes.Buffer
	if err := Write(&buf, s, "json"); err != nil {
		t.Fatal(err)
	}
	var got struct{ Tests []string }
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil || len(got.Tests) != 2 {
		t.Errorf("json tests = %q (%v)", got.Tests, err)
	}

	// knowledge/decisions/pr-evidence-summary.md: the summary states only
	// what the report says, so a run that named nothing lists no selection.
	opt.Tests, opt.MutationTests = nil, []string{"Ns.A"}
	if _, _, err := verify.Run(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	if s, err = Build(Options{Root: root, Tier: "prepr", Base: "main"}); err != nil {
		t.Fatal(err)
	}
	if md := Markdown(s); strings.Contains(md, "- Tests named") || !strings.Contains(md, "- Mutation tests named (`--mutation-tests`): `Ns.A`.") {
		t.Errorf("mutation tests named, tests not:\n%s", md)
	}
}

// knowledge/decisions/report-profile-path.md: the summary names the
// profile file the run loaded and the one it judged by, a file the default
// profile protects judges its own report, and the list of protected-path
// changes always reads the default profile (protected-path-approval.md).
func TestSummaryNamesTheProfileFile(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\"snapshots/**\", \"alt.yaml\"]\ncommands:\n  build: \"true\"\n")
	second := "schemaVersion: 0.1.0\nname: alt\nstarting_condition: greenfield\n" +
		"paths: { product: [\"src/**\"], tests: [\"test/**\"], ignore: [\"docs/**\"] }\n" + testrepo.Roles +
		"commands:\n  build: \"true\"\ntiers:\n  prepr: { run: [build] }\n"
	testrepo.Write(t, root, "alt.yaml", second)
	testrepo.Git(t, root, "add", "alt.yaml")
	testrepo.Git(t, root, "commit", "-qm", "alt profile")
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, "snapshots/s.txt", "changed")
	testrepo.Write(t, root, "other.yaml", second)
	// The branch's default profile protects notes/ too; alt.yaml protects
	// nothing.
	def, err := os.ReadFile(filepath.Join(root, "reliability-profile.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Write(t, root, "reliability-profile.yaml", strings.Replace(string(def), "\"alt.yaml\"]", "\"alt.yaml\", \"notes/**\"]", 1))
	testrepo.Write(t, root, "notes/n.txt", "a note")
	prepr := func(prof string) *Summary {
		t.Helper()
		if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: prof, Tier: "prepr"}); err != nil {
			t.Fatal(err)
		}
		return build(t, root)
	}

	s := prepr("alt.yaml")
	md := Markdown(s)
	if !strings.Contains(md, "- Profile file `alt.yaml`; judged by `alt.yaml`.\n") {
		t.Errorf("markdown does not name the protected profile file as its judge:\n%s", md)
	}
	protected := map[string]bool{}
	for _, c := range s.Protected {
		protected[c.Path] = true
	}
	if !protected["snapshots/s.txt"] || !protected["notes/n.txt"] {
		t.Errorf("protected = %+v, want snapshots/s.txt and notes/n.txt from the default profile", s.Protected)
	}

	md = Markdown(prepr("other.yaml"))
	if !strings.Contains(md, "- Profile file `other.yaml`; judged by `reliability-profile.yaml`, since `reliability-profile.yaml` does not protect it.\n") {
		t.Errorf("markdown does not say the default judged an unprotected file:\n%s", md)
	}
}
