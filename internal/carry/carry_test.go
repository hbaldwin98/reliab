package carry

import (
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

// Reports in, findings out: the carry rule of
// knowledge/decisions/finding-classification.md ("A decision holds in every
// tier and through runs without the finding", "A command-level finding
// carries only while its failure is the same", environment never carries)
// and knowledge/decisions/verify-execution.md (the newest decision for each
// id carries).

const id = "F-unit-abc"

func run(tier, started string, findings ...report.Finding) *report.Report {
	return &report.Report{Run: report.Run{Tier: tier, StartedAt: started}, Findings: findings}
}

func classified(kind, reason, at string) report.Finding {
	return report.Finding{ID: id, Test: "pkg.TestA", Kind: kind, Status: report.StatusOpen,
		Classification: &report.Classification{Reason: reason, At: at}}
}

func acked(by, at string) report.Finding {
	f := classified(report.KindLegacyDefectOrQuirk, "known", "2026-10-20T09:00:00Z")
	f.Acknowledged, f.Status = &report.Acknowledged{By: by, At: at}, report.StatusAcknowledged
	return f
}

func fresh() report.Finding {
	return report.Finding{ID: id, Test: "pkg.TestA", Kind: report.KindUnclassified, Status: report.StatusOpen}
}

func decide(past []*report.Report, f report.Finding) report.Finding {
	rep := run("inner", "2026-10-21T00:00:00Z", f)
	Collect(past).Apply(rep)
	return rep.Findings[0]
}

// verify-execution.md: timestamps compare as moments, whatever their zones;
// on a tie the later record read wins; one that does not parse (a report
// edited by hand) never beats one that does; between two that do not, the
// later record read wins.
func TestNewestClassificationWins(t *testing.T) {
	tests := []struct {
		name          string
		first, second string // At of the classification read first and second
		want          string // reason of the one that carries
	}{
		{"a later moment read second wins", "2026-10-20T12:00:00Z", "2026-10-20T10:00:00-07:00", "second"},
		{"a later moment read first wins", "2026-10-20T10:00:00-07:00", "2026-10-20T12:00:00Z", "first"},
		{"a tie goes to the later read", "2026-10-20T10:00:00Z", "2026-10-20T10:00:00Z", "second"},
		{"a parsed time beats a garbled one read later", "2026-10-20T10:00:00Z", "garbled", "first"},
		{"a parsed time read later beats a garbled one", "garbled", "2026-10-20T10:00:00Z", "second"},
		{"between garbled ones the later read wins", "garbled", "also garbled", "second"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			past := []*report.Report{
				run("prepr", "2026-10-20T08:00:00Z", classified(report.KindLegacyDefectOrQuirk, "first", tt.first)),
				run("inner", "2026-10-20T09:00:00Z", classified(report.KindRequirementSilent, "second", tt.second)),
			}
			got := decide(past, fresh())
			if got.Classification == nil || got.Classification.Reason != tt.want {
				t.Fatalf("carried %+v, want %s", got.Classification, tt.want)
			}
			wantKind := map[string]string{"first": report.KindLegacyDefectOrQuirk, "second": report.KindRequirementSilent}[tt.want]
			if got.Kind != wantKind {
				t.Errorf("kind = %s, want %s", got.Kind, wantKind)
			}
		})
	}
}

// Acknowledgements follow the same rule: 10:00-07:00 is 17:00 UTC, later
// than 12:00 UTC though it reads earlier as text.
func TestAcknowledgementNewestByMoment(t *testing.T) {
	past := []*report.Report{
		run("prepr", "2026-10-20T08:00:00Z", acked("human:late", "2026-10-20T10:00:00-07:00")),
		run("inner", "2026-10-20T09:00:00Z", acked("human:early", "2026-10-20T12:00:00Z")),
	}
	got := decide(past, fresh())
	if got.Acknowledged == nil || got.Acknowledged.By != "human:late" || got.Status != report.StatusAcknowledged {
		t.Errorf("acknowledged = %+v, status %s; want human:late's (17:00 UTC beats 12:00 UTC)", got.Acknowledged, got.Status)
	}
}

// finding-classification.md: a carried decision records the run it was made
// in as from (tier and run start), kept through later carries; one made on
// the report itself names none.
func TestFromNamesTheRun(t *testing.T) {
	past := []*report.Report{run("prepr", "2026-10-20T08:00:00Z", acked("human:owner", "2026-10-20T09:30:00Z"))}
	got := decide(past, fresh())
	if got.Classification.From != "prepr 2026-10-20T08:00:00Z" || got.Acknowledged.From != "prepr 2026-10-20T08:00:00Z" {
		t.Errorf("from = %q, %q", got.Classification.From, got.Acknowledged.From)
	}

	// Carried again: the original run is kept.
	again := decide([]*report.Report{run("inner", "2026-10-21T00:00:00Z", got)}, fresh())
	if again.Classification.From != "prepr 2026-10-20T08:00:00Z" || again.Acknowledged.From != "prepr 2026-10-20T08:00:00Z" {
		t.Errorf("carried twice: from = %q, %q", again.Classification.From, again.Acknowledged.From)
	}

	// Read from the report's own run (a rescore in place): no from.
	own := run("prepr", "2026-10-20T08:00:00Z", acked("human:owner", "2026-10-20T09:30:00Z"))
	rep := run("prepr", "2026-10-20T08:00:00Z", fresh())
	Collect([]*report.Report{own}).Apply(rep)
	if f := rep.Findings[0]; f.Classification.From != "" || f.Acknowledged.From != "" {
		t.Errorf("own run: from = %q, %q", f.Classification.From, f.Acknowledged.From)
	}
}

// finding-classification.md: environment never carries.
func TestEnvironmentDoesNotCarry(t *testing.T) {
	past := []*report.Report{run("inner", "2026-10-20T08:00:00Z", classified(report.KindEnvironment, "out of consoles", "2026-10-20T08:10:00Z"))}
	if got := decide(past, fresh()); got.Kind != report.KindUnclassified || got.Classification != nil || got.Reopened != "" {
		t.Errorf("environment carried: %+v", got)
	}
}

// finding-classification.md: a command-level finding carries only while its
// fingerprint is the same; a report without one carries nothing; a finding
// with a test carries by id alone.
func TestCommandLevelFingerprint(t *testing.T) {
	cmd := func(fp string) report.Finding {
		return report.Finding{ID: "F-analyzers", Kind: report.KindUnclassified, Status: report.StatusOpen, Fingerprint: fp}
	}
	old := func(fp string) report.Finding {
		f := cmd(fp)
		f.Kind = report.KindLegacyDefectOrQuirk
		f.Classification = &report.Classification{Reason: "known", At: "2026-10-20T08:10:00Z"}
		return f
	}
	tests := []struct {
		name, oldFP, curFP string
		carries            bool
		reopened           string
	}{
		{"same failure", "aaa", "aaa", true, ""},
		{"changed failure", "aaa", "bbb", false, "the failure changed since it was classified legacy-defect-or-quirk; judge it afresh"},
		{"no fingerprint recorded", "", "aaa", false, "recorded no fingerprint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := run("inner", "2026-10-21T00:00:00Z", cmd(tt.curFP))
			Collect([]*report.Report{run("inner", "2026-10-20T08:00:00Z", old(tt.oldFP))}).Apply(rep)
			got := rep.Findings[0]
			if carried := got.Classification != nil; carried != tt.carries {
				t.Errorf("carried = %v, want %v: %+v", carried, tt.carries, got)
			}
			if tt.reopened == "" && got.Reopened != "" || !strings.Contains(got.Reopened, tt.reopened) {
				t.Errorf("reopened = %q, want %q", got.Reopened, tt.reopened)
			}
		})
	}
	// A finding with a test keeps its decision whatever its fingerprint.
	f := fresh()
	f.Fingerprint = "zzz"
	if got := decide([]*report.Report{run("inner", "2026-10-20T08:00:00Z", classified(report.KindLegacyDefectOrQuirk, "known", "2026-10-20T08:10:00Z"))}, f); got.Classification == nil || got.Reopened != "" {
		t.Errorf("test finding: %+v", got)
	}
}

// Rescoring in place (triage after a classify or ack) carries from the
// report itself: a finding that already holds a decision is left as it is,
// so carrying twice, or from the report's own run, changes nothing, even
// for a command-level finding from before fingerprints.
func TestApplyingTwiceChangesNothing(t *testing.T) {
	f := report.Finding{ID: "F-analyzers", Kind: report.KindLegacyDefectOrQuirk, Status: report.StatusOpen,
		Classification: &report.Classification{Reason: "known", At: "2026-10-20T08:10:00Z"}}
	rep := run("inner", "2026-10-20T08:00:00Z", f)
	d := Collect([]*report.Report{rep})
	d.Apply(rep)
	d.Apply(rep)
	if got := rep.Findings[0]; got.Reopened != "" || got.Classification == nil || got.Classification.From != "" || got.Kind != report.KindLegacyDefectOrQuirk {
		t.Errorf("after applying its own decisions: %+v", got)
	}
}

// An id no report decided is left alone.
func TestUndecidedIsUntouched(t *testing.T) {
	other := classified(report.KindLegacyDefectOrQuirk, "known", "2026-10-20T08:10:00Z")
	other.ID = "F-unit-other"
	if got := decide([]*report.Report{run("inner", "2026-10-20T08:00:00Z", other)}, fresh()); got != fresh() {
		t.Errorf("undecided finding changed: %+v", got)
	}
}

// finding-classification.md: the newest acknowledgement recorded for an id
// carries. A finding that already holds an older one takes the newer; only
// the same acknowledgement counts as already held.
func TestHeldOlderAcknowledgementTakesTheNewer(t *testing.T) {
	cur := acked("human:early", "2026-10-20T09:00:00Z")
	past := []*report.Report{run("prepr", "2026-10-20T08:00:00Z", acked("human:late", "2026-10-20T10:00:00Z"))}
	if got := decide(past, cur); got.Acknowledged == nil || got.Acknowledged.By != "human:late" {
		t.Errorf("acknowledged = %+v, want human:late's newer one", got.Acknowledged)
	}
}
