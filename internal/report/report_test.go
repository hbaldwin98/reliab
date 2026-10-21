package report

import (
	"os"
	"path/filepath"
	"testing"
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
