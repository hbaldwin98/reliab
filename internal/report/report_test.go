package report

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

func minimal(tier, started string) *Report {
	return &Report{
		SchemaVersion: SchemaVersion, Tool: Tool{Name: "reliab", Version: "dev"},
		Run: Run{Tier: tier, Profile: "p", StartedAt: started, TreeHash: "0123456789012345678901234567890123456789",
			Commands: []CommandResult{}, Skipped: []Skip{}},
		Findings: []Finding{},
	}
}

// knowledge/decisions/report-storage.md: a run is written as its tier's
// latest report and into history, and a rewrite (classify, ack, verdict)
// replaces both, since history is the durable record.
func TestWriteAndRewriteKeepHistory(t *testing.T) {
	root := t.TempDir()
	r := minimal("prepr", "2026-10-18T10:00:00Z")
	latest, err := Write(root, r)
	if err != nil || latest != LatestPath(root, "prepr") {
		t.Fatalf("Write = %q, %v", latest, err)
	}
	r.Verdict = &Verdict{Outcome: VerdictFail, By: "reliab:reviewer", At: "2026-10-18T11:00:00Z"}
	if err := Rewrite(root, r); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{LatestPath(root, "prepr"), HistoryPath(root, r)} {
		got, err := Read(p)
		if err != nil || got.Verdict == nil || got.Verdict.Outcome != VerdictFail {
			t.Errorf("%s: %+v, %v", p, got, err)
		}
	}
	r.Verdict = nil
	if err := RewriteHistory(root, r); err != nil {
		t.Fatal(err)
	}
	if got, _ := Read(HistoryPath(root, r)); got == nil || got.Verdict != nil {
		t.Errorf("RewriteHistory left the history copy: %+v", got)
	}
	if got, _ := Read(LatestPath(root, "prepr")); got == nil || got.Verdict == nil {
		t.Errorf("RewriteHistory changed the latest report: %+v", got)
	}
}

// The PR summary reads the history oldest first, so for each finding the
// latest earlier run wins (knowledge/decisions/pr-evidence-summary.md).
func TestHistory(t *testing.T) {
	root := t.TempDir()
	if h := History(root); len(h) != 0 {
		t.Fatalf("no history: %d reports", len(h))
	}
	for _, r := range []*Report{
		minimal("prepr", "2026-10-18T10:00:00Z"),
		minimal("inner", "2026-10-18T09:00:00Z"),
		minimal("inner", "2026-10-18T11:00:00Z"),
	} {
		if _, err := Write(root, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, Dir, "history", "inner-broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := History(root)
	var got []string
	for _, r := range h {
		got = append(got, r.Run.StartedAt)
	}
	want := []string{"2026-10-18T09:00:00Z", "2026-10-18T10:00:00Z", "2026-10-18T11:00:00Z"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("history = %v, want %v (oldest first, unreadable skipped)", got, want)
	}
}

// knowledge/decisions/named-tests.md: the hooks and verify's exit wait on
// the skips with reason needs-input, and only those.
func TestNeedsInput(t *testing.T) {
	r := minimal("inner", "2026-10-20T10:00:00Z")
	if got := r.NeedsInput(); len(got) != 0 {
		t.Errorf("no skips: %+v", got)
	}
	r.Run.Skipped = []Skip{
		{ID: "build", Reason: SkipBudget},
		{ID: "store_tests", Reason: SkipNeedsInput, Detail: "no tests named: re-run with --tests"},
		{ID: "faults_db", Reason: SkipNotRelevant},
		{ID: "mutation_changed", Reason: SkipNeedsInput, Detail: "no mutation tests named: re-run with --mutation-tests"},
	}
	got := r.NeedsInput()
	if len(got) != 2 || got[0].ID != "store_tests" || got[1].ID != "mutation_changed" {
		t.Errorf("NeedsInput = %+v", got)
	}
}

// knowledge/decisions/reviewer-verdict.md: the verdict records the author's
// report tree, and only a verdict for that tree counts; a report with no
// verdict has none current.
func TestVerdictCurrent(t *testing.T) {
	r := minimal("prepr", "2026-10-18T10:00:00Z")
	if r.VerdictCurrent() {
		t.Error("no verdict counted as current")
	}
	r.Verdict = &Verdict{Outcome: VerdictPass, By: "reliab:reviewer", TreeHash: r.Run.TreeHash}
	if !r.VerdictCurrent() {
		t.Error("a verdict on the report's tree is not current")
	}
	r.Verdict.TreeHash = "fedcba9876543210fedcba9876543210fedcba98"
	if r.VerdictCurrent() {
		t.Error("a verdict on another tree counted as current")
	}
}

// knowledge/decisions/schema-v0-shape.md (report 0.12.0): a report names
// the profile file its run loaded, relative to the repo root unless
// outside it; a report written before then is read with the default.
func TestProfileFile(t *testing.T) {
	root := t.TempDir()
	r := minimal("prepr", "2026-10-18T10:00:00Z")
	if got, want := r.ProfileFile(root, "reliability-profile.yaml"), filepath.Join(root, "reliability-profile.yaml"); got != want {
		t.Errorf("no profilePath: %q, want %q", got, want)
	}
	r.Run.ProfilePath = "ci/alt.yaml"
	if got, want := r.ProfileFile(root, "reliability-profile.yaml"), filepath.Join(root, "ci", "alt.yaml"); got != want {
		t.Errorf("relative: %q, want %q", got, want)
	}
	abs := filepath.Join(t.TempDir(), "alt.yaml")
	r.Run.ProfilePath = filepath.ToSlash(abs)
	if got := r.ProfileFile(root, "reliability-profile.yaml"); got != abs {
		t.Errorf("absolute: %q, want %q", got, abs)
	}
	if err := r.Validate(); err != nil {
		t.Errorf("schema: %v", err)
	}
}

// knowledge/decisions/report-profile-path.md: the file a run loaded judges
// its report when reliability-profile.yaml protects it; an unprotected file
// does not, and the default judges instead.
func TestJudgeProfile(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\"ci/alt.yaml\"]\ncommands:\n  build: \"true\"\n")
	alt := "schemaVersion: 0.1.0\nname: alt\nstarting_condition: greenfield\n" +
		"paths: { product: [\"src/**\"], tests: [\"test/**\"], ignore: [\"docs/**\"] }\n" + testrepo.Roles +
		"commands:\n  build: \"true\"\n"
	testrepo.Write(t, root, "ci/alt.yaml", alt)
	testrepo.Write(t, root, "other.yaml", alt)
	r := minimal("prepr", "2026-10-18T10:00:00Z")
	r.Run.ProfilePath = "ci/alt.yaml"
	if p, path, err := r.JudgeProfile(root); err != nil || path != "ci/alt.yaml" || p.Name != "alt" {
		t.Errorf("protected file: %v, %q, %v; want ci/alt.yaml to judge", p, path, err)
	}
	r.Run.ProfilePath = "other.yaml"
	if p, path, err := r.JudgeProfile(root); err != nil || path != "reliability-profile.yaml" || p.Name != "test" {
		t.Errorf("unprotected file: %v, %q, %v; want the default to judge", p, path, err)
	}
}

// knowledge/decisions/pr-evidence-summary.md: the fixed list reads the runs
// whose head lies between the merge base with the base and HEAD, so runs
// from another branch in the same checkout do not count;
// finding-classification.md: classify picks among the same runs.
func TestBranchHistory(t *testing.T) {
	root := testrepo.New(t, "")
	commit := func(name string) string {
		t.Helper()
		testrepo.Write(t, root, "src/"+name+".txt", name)
		testrepo.Git(t, root, "add", "-A")
		testrepo.Git(t, root, "commit", "-qm", name)
		return gitx.Head(root)
	}
	record := func(head, started string) {
		t.Helper()
		r := minimal("prepr", started)
		r.Run.Head = head
		if _, err := Write(root, r); err != nil {
			t.Fatal(err)
		}
	}
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	record(commit("feature"), "2026-10-18T10:00:00Z")
	testrepo.Git(t, root, "checkout", "-q", "-b", "other", "main")
	record(commit("other"), "2026-10-18T11:00:00Z")
	testrepo.Git(t, root, "checkout", "-q", "feature")

	got, base, err := BranchHistory(root, "main", "", time.Time{})
	if err != nil || base != "main" || len(got) != 1 || got[0].Run.StartedAt != "2026-10-18T10:00:00Z" {
		t.Fatalf("BranchHistory = %d run(s), %q, %v; want the feature run only", len(got), base, err)
	}
	if got, _, err := BranchHistory(root, "main", "", time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)); err != nil || len(got) != 0 {
		t.Errorf("before the feature run: %d run(s), %v; want none", len(got), err)
// knowledge/decisions/report-freshness.md: a report vouches for the tree it
// ran on, and for no tree once the tree changed while it ran, even the tree
// it started on.
func TestFreshFor(t *testing.T) {
	r := minimal("inner", "2026-10-20T09:00:00Z")
	start, other := r.Run.TreeHash, "fedcba9876543210fedcba9876543210fedcba98"
	if !r.FreshFor(start) {
		t.Error("a report is not fresh for the tree it ran on")
	}
	if r.FreshFor(other) {
		t.Error("a report is fresh for a tree it did not run on")
	}
	r.Run.TreeHashEnd = other
	if r.FreshFor(start) || r.FreshFor(other) {
		t.Error("a report whose tree changed during the run is fresh for its start or end tree")
	}
}
