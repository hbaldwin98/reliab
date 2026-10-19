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
