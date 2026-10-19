package mutscore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
)

// Every rule tested here is stated in knowledge/decisions/mutation-threshold.md.

func threshold(v float64) *profile.Profile {
	return &profile.Profile{Commands: map[string]profile.Command{"mutation_changed": {Run: "x", Threshold: &v}}}
}

func survivor(test, loc, kind, status string) report.Finding {
	f := report.Finding{ID: "F-mutation-changed-" + test, Kind: kind, Command: "mutation_changed", Test: test, Location: loc, Status: status}
	if status == report.StatusAcknowledged {
		f.Acknowledged = &report.Acknowledged{By: "human:t", At: "2026-10-18T00:00:00Z"}
	}
	return f
}

func mutationReport(c report.MutantCounts, files map[string]report.FileCounts, fs ...report.Finding) *report.Report {
	return &report.Report{
		Run:      report.Run{Tier: "prepr", TreeHash: strings.Repeat("a", 40), Commands: []report.CommandResult{{ID: "mutation_changed", Mutants: &c, MutantFiles: files}}},
		Findings: fs,
	}
}

func shortfalls(r *report.Report) []report.Finding {
	var out []report.Finding
	for _, f := range r.Findings {
		if f.Kind == report.KindBelowThreshold {
			out = append(out, f)
		}
	}
	return out
}

// The rule: killed over killed, survived, and not covered; a confirmed
// equivalent leaves the count, an unconfirmed one and a requirement-silent
// survivor stay in it.
func TestComputeCountsPerRule(t *testing.T) {
	r := mutationReport(report.MutantCounts{Killed: 6, Survived: 3, NotCovered: 1}, nil,
		survivor("a", "src/a.go:1:1", report.KindEquivalentMutant, report.StatusAcknowledged),
		survivor("b", "src/a.go:2:1", report.KindEquivalentMutant, report.StatusOpen),
		survivor("c", "src/a.go:3:1", report.KindRequirementSilent, report.StatusOpen),
		survivor("d", "src/a.go:4:1", report.KindUnclassified, report.StatusOpen),
	)
	s := Compute(r.Run.Commands[0], r.Findings, threshold(80), nil)
	if s.Killed != 6 || s.Counted != 9 || s.Equivalent != 1 || s.PendingEquivalent != 1 || *s.Threshold != 80 {
		t.Fatalf("score = %+v, want 6 of 9 with 1 confirmed and 1 pending equivalent", s)
	}
	if got := Compute(report.CommandResult{ID: "mutation_changed"}, nil, threshold(80), nil); got != nil {
		t.Errorf("no counts scored %+v, want nil: nothing measured is not a score", got)
	}
}

// Short is the fewest kills that reach the threshold.
func TestShortIsMinimal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		counted := rapid.IntRange(0, 500).Draw(t, "counted")
		killed := rapid.IntRange(0, counted).Draw(t, "killed")
		want := float64(rapid.IntRange(0, 1000).Draw(t, "want10")) / 10
		ch := Check{Want: want, Killed: killed, Counted: counted}
		n := ch.Short()
		if report.Below(killed+n, counted, want) && killed+n <= counted {
			t.Fatalf("killing %d more still leaves %d of %d below %v", n, killed+n, counted, want)
		}
		if n > 0 && !report.Below(killed+n-1, counted, want) {
			t.Fatalf("short by %d, but %d more already reach %v of %d", n, n-1, want, counted)
		}
		if (n > 0) != ch.Below() {
			t.Fatalf("short by %d but Below = %v", n, ch.Below())
		}
	})
}

func TestApplyAddsAShortfallWithTheNumbers(t *testing.T) {
	r := mutationReport(report.MutantCounts{Killed: 7, Survived: 2, NotCovered: 1}, nil,
		survivor("a", "src/a.go:1:1", report.KindUnclassified, report.StatusOpen),
		survivor("b", "src/a.go:2:1", report.KindRequirementSilent, report.StatusOpen),
		survivor("c", "src/a.go:3:1", report.KindUnclassified, report.StatusOpen),
	)
	Apply(r, threshold(80), nil)
	got := shortfalls(r)
	if len(got) != 1 {
		t.Fatalf("shortfalls = %+v, want one", got)
	}
	f := got[0]
	if f.Status != report.StatusOpen || f.Command != "mutation_changed" || f.Test != "mutation score 70.0% below the threshold 80% (7 killed of 10 counted)" {
		t.Errorf("shortfall = %+v", f)
	}
	for _, want := range []string{"Short by 1", "7 killed, 2 survived, 1 not covered", "F-mutation-changed-a [unclassified]", "F-mutation-changed-b [requirement-silent]", "Never write a test only to raise the score"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("message lacks %q:\n%s", want, f.Message)
		}
	}
	if len(r.Findings) != 4 {
		t.Errorf("findings = %d, want the 3 survivors kept and the shortfall", len(r.Findings))
	}
	// A second Apply replaces the shortfall rather than adding another.
	Apply(r, threshold(80), nil)
	if len(shortfalls(r)) != 1 || len(r.Findings) != 4 {
		t.Errorf("after re-apply: %d findings, %d shortfalls", len(r.Findings), len(shortfalls(r)))
	}
}

func TestApplyMetOrUnsetAddsNothing(t *testing.T) {
	r := mutationReport(report.MutantCounts{Killed: 8, Survived: 2}, nil)
	Apply(r, threshold(80), nil)
	if len(shortfalls(r)) != 0 || r.Run.Commands[0].Score == nil {
		t.Errorf("at the threshold: shortfalls %+v, score %+v", shortfalls(r), r.Run.Commands[0].Score)
	}
	r = mutationReport(report.MutantCounts{Survived: 5}, nil)
	Apply(r, &profile.Profile{}, nil)
	if len(shortfalls(r)) != 0 || r.Run.Commands[0].Score.Threshold != nil {
		t.Errorf("no threshold: shortfalls %+v, score %+v", shortfalls(r), r.Run.Commands[0].Score)
	}
}

// A person's acknowledgement holds only while the numbers are the same:
// accepting 70% once does not accept 50% later.
func TestApplyKeepsAnAcknowledgementOnlyForTheSameNumbers(t *testing.T) {
	r := mutationReport(report.MutantCounts{Killed: 7, Survived: 3}, nil)
	Apply(r, threshold(80), nil)
	f := &r.Findings[0]
	f.Acknowledged, f.Status = &report.Acknowledged{By: "human:t", At: "2026-10-18T00:00:00Z"}, report.StatusAcknowledged
	Apply(r, threshold(80), nil)
	if g := shortfalls(r)[0]; g.Status != report.StatusAcknowledged {
		t.Errorf("same numbers: %+v, want still acknowledged", g)
	}
	r.Run.Commands[0].Mutants.Killed = 5
	Apply(r, threshold(80), nil)
	if g := shortfalls(r)[0]; g.Status != report.StatusOpen || g.Acknowledged != nil {
		t.Errorf("new numbers: %+v, want a new open finding", g)
	}
}

// A confirmed equivalent leaves the count, which can lift the score over
// the threshold.
func TestApplyConfirmedEquivalentCanMeetTheThreshold(t *testing.T) {
	r := mutationReport(report.MutantCounts{Killed: 4, Survived: 1}, nil,
		survivor("a", "src/a.go:1:1", report.KindEquivalentMutant, report.StatusOpen))
	Apply(r, threshold(100), nil)
	if len(shortfalls(r)) != 1 {
		t.Fatalf("pending equivalent: want a shortfall, got %+v", r.Findings)
	}
	r.Findings[0] = survivor("a", "src/a.go:1:1", report.KindEquivalentMutant, report.StatusAcknowledged)
	Apply(r, threshold(100), nil)
	if s := r.Run.Commands[0].Score; len(shortfalls(r)) != 0 || s.Counted != 4 || s.Equivalent != 1 {
		t.Errorf("confirmed: shortfalls %+v, score %+v", shortfalls(r), s)
	}
}

func TestBaselineScopesScorePerFile(t *testing.T) {
	files := map[string]report.FileCounts{
		"src/billing/a.go": {Killed: 1, Survived: 2},
		"src/billing/b.go": {Killed: 1},
		"src/other/c.go":   {Killed: 5},
	}
	r := mutationReport(report.MutantCounts{Killed: 7, Survived: 2}, files,
		survivor("a", "src/billing/a.go:3:1", report.KindUnclassified, report.StatusOpen),
		survivor("b", "src/billing/a.go:4:1", report.KindUnclassified, report.StatusOpen))
	b := &Baseline{Scopes: []Scope{{Path: "src/billing/**", Score: 60}, {Path: "src/other/**", Score: 90}, {Path: "docs/**", Score: 50}}}
	Apply(r, &profile.Profile{}, b)
	s := r.Run.Commands[0].Score
	if len(s.Baselines) != 2 || s.Baselines[0] != (report.ScopeScore{Scope: "src/billing/**", Baseline: 60, Killed: 2, Counted: 4}) {
		t.Fatalf("baselines = %+v, want billing 2 of 4 and other; docs measured nothing", s.Baselines)
	}
	got := shortfalls(r)
	if len(got) != 1 || got[0].Test != "mutation score in src/billing/** 50.0% below its baseline 60% (2 killed of 4 counted)" {
		t.Fatalf("shortfalls = %+v", got)
	}
	if !strings.Contains(got[0].Message, "F-mutation-changed-a") {
		t.Errorf("message does not name the scope's survivors: %s", got[0].Message)
	}

	// Without per-file counts a scope cannot be scored, and says so.
	r = mutationReport(report.MutantCounts{Killed: 7, Survived: 2}, nil)
	Apply(r, &profile.Profile{}, b)
	if bl := r.Run.Commands[0].Score.Baselines; len(bl) != 3 || !bl[0].Unmeasured {
		t.Errorf("no per-file counts: %+v, want every scope unmeasured", bl)
	}
	if len(shortfalls(r)) != 0 {
		t.Errorf("an unmeasured scope is not a shortfall: %+v", shortfalls(r))
	}
}

func TestSurvivorsExcludeConfirmedAndCommandFailures(t *testing.T) {
	fs := []report.Finding{
		survivor("a", "src/a.go:1:1", report.KindUnclassified, report.StatusOpen),
		survivor("b", "src/a.go:2:1", report.KindEquivalentMutant, report.StatusAcknowledged),
		{ID: "F-mutation-changed", Command: "mutation_changed", Kind: report.KindUnclassified, Status: report.StatusOpen},
		{ID: "F-unit-x", Command: "unit", Test: "x", Kind: report.KindUnclassified, Status: report.StatusOpen},
	}
	got := Survivors(Check{Command: "mutation_changed"}, fs)
	if len(got) != 1 || got[0].Test != "a" {
		t.Errorf("survivors = %+v, want only a", got)
	}
}

func TestProposeRecordsAndRatchets(t *testing.T) {
	files := map[string]report.FileCounts{"src/billing/a.go": {Killed: 3, Survived: 1}, "src/x/b.go": {Killed: 1, Survived: 1}}
	r := mutationReport(report.MutantCounts{Killed: 4, Survived: 2}, files)
	r.Run.Commands[0].ID = "mutation_full"
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	next, changes, err := Propose(nil, r, "mutation_full", []string{"src/billing/**"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Scopes) != 1 || next.Scopes[0].Score != 75 || next.Scopes[0].Counted != 4 || next.Scopes[0].TreeHash != r.Run.TreeHash || changes[0].Old != nil {
		t.Fatalf("proposed %+v, changes %+v", next, changes)
	}
	if _, err := next.Marshal(); err != nil {
		t.Fatalf("proposed baseline does not validate: %v", err)
	}

	cur := &Baseline{SchemaVersion: BaselineVersion, Scopes: []Scope{{Path: "src/billing/**", Score: 80}, {Path: "src/keep/**", Score: 90}}}
	next, changes, err = Propose(cur, r, "mutation_full", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	// Re-recording every scope: billing drops from 80 to 75, a lowering the
	// ratchet must flag; keep measured nothing this run, so it keeps 90.
	if len(changes) != 1 || changes[0].Path != "src/billing/**" || !changes[0].Lower() {
		t.Fatalf("re-recording existing scopes: changes %+v", changes)
	}
	if len(next.Scopes) != 2 || next.Scopes[1].Path != "src/keep/**" || next.Scopes[1].Score != 90 {
		t.Errorf("an unmeasured scope keeps its score: %+v", next.Scopes)
	}
	if _, _, err := Propose(cur, r, "mutation_full", []string{"src/keep/**"}, now); err == nil {
		t.Error("a scope named on purpose with no mutants was accepted")
	}
}

func TestProposeRefusals(t *testing.T) {
	now := time.Now()
	r := mutationReport(report.MutantCounts{Killed: 1}, nil)
	if _, _, err := Propose(nil, r, "mutation_changed", []string{"src/**"}, now); err == nil || !strings.Contains(err.Error(), "per-file") {
		t.Errorf("no per-file counts: err = %v", err)
	}
	if _, _, err := Propose(nil, r, "mutation_full", []string{"src/**"}, now); err == nil || !strings.Contains(err.Error(), "did not run") {
		t.Errorf("command not in the report: err = %v", err)
	}
	r = mutationReport(report.MutantCounts{Killed: 1}, map[string]report.FileCounts{"src/a.go": {Killed: 1}})
	if _, _, err := Propose(nil, r, "mutation_changed", nil, now); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Errorf("no scopes: err = %v", err)
	}
	if _, _, err := Propose(nil, r, "mutation_changed", []string{"docs/**"}, now); err == nil || !strings.Contains(err.Error(), "no mutants") {
		t.Errorf("empty scope: err = %v", err)
	}
}

func TestParseBaseline(t *testing.T) {
	if _, err := ParseBaseline([]byte(`{"schemaVersion":"0.1.0","scopes":[{"path":"src/**","score":72.5}]}`)); err != nil {
		t.Errorf("valid baseline: %v", err)
	}
	for name, raw := range map[string]string{
		"score over 100": `{"schemaVersion":"0.1.0","scopes":[{"path":"src/**","score":101}]}`,
		"bad glob":       `{"schemaVersion":"0.1.0","scopes":[{"path":"src/[","score":1}]}`,
		"unknown key":    `{"schemaVersion":"0.1.0","scopes":[],"x":1}`,
		"future major":   `{"schemaVersion":"1.0.0","scopes":[]}`,
	} {
		if _, err := ParseBaseline([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFileOf(t *testing.T) {
	for loc, want := range map[string]string{"src/a.go:12:3": "src/a.go", "src/a.go:12": "src/a.go", "src/a.go": "src/a.go", "": ""} {
		if got := fileOf(loc); got != want {
			t.Errorf("fileOf(%q) = %q, want %q", loc, got, want)
		}
	}
}

// The score rule holds inside a scope: not covered mutants there count, and
// confirmed equivalents there leave the count (mutation-threshold.md).
func TestScopeScoreFollowsTheRule(t *testing.T) {
	files := map[string]report.FileCounts{"src/billing/a.go": {Killed: 4, Survived: 1, NotCovered: 2}}
	r := mutationReport(report.MutantCounts{Killed: 4, Survived: 1, NotCovered: 2}, files,
		survivor("a", "src/billing/a.go:3:1", report.KindEquivalentMutant, report.StatusAcknowledged))
	Apply(r, &profile.Profile{}, &Baseline{Scopes: []Scope{{Path: "src/billing/**", Score: 10}}})
	if got := r.Run.Commands[0].Score.Baselines; len(got) != 1 || got[0].Killed != 4 || got[0].Counted != 6 {
		t.Errorf("scope = %+v, want 4 killed of 6 counted: 4+1+2 less one confirmed equivalent", got)
	}
}

// The shortfall's message gives the numbers the score came from (the
// finding "with the numbers"), and says when it counts no survivor
// findings rather than listing none (mutation-threshold.md).
func TestShortfallMessageCarriesTheNumbers(t *testing.T) {
	r := mutationReport(report.MutantCounts{Killed: 3, Survived: 2}, nil,
		survivor("a", "src/a.go:1:1", report.KindEquivalentMutant, report.StatusAcknowledged))
	Apply(r, threshold(100), nil)
	msg := shortfalls(r)[0].Message
	if !strings.Contains(msg, "3 killed, 2 survived, 0 not covered, less 1 confirmed equivalent.") {
		t.Errorf("message lacks the confirmed equivalent:\n%s", msg)
	}
	if !strings.Contains(msg, "No survivor findings are counted here") {
		t.Errorf("a shortfall with no survivor findings does not say so:\n%s", msg)
	}

	r = mutationReport(report.MutantCounts{Killed: 3, Survived: 1}, nil,
		survivor("b", "src/a.go:2:1", report.KindUnclassified, report.StatusOpen))
	Apply(r, threshold(100), nil)
	msg = shortfalls(r)[0].Message
	if strings.Contains(msg, "less") || strings.Contains(msg, "No survivor findings") || !strings.Contains(msg, "Survivors counted: F-mutation-changed-b") {
		t.Errorf("no equivalents, one survivor:\n%s", msg)
	}
}

// A profile that names no baseline file, or one not written yet, holds
// runs to no scope; a written one is read; an invalid one is a config
// error, so a broken ratchet never passes (mutation-threshold.md).
func TestLoadBaseline(t *testing.T) {
	root := t.TempDir()
	if b, err := LoadBaseline(root, &profile.Profile{}); b != nil || err != nil {
		t.Errorf("no baseline named: %+v, %v", b, err)
	}
	p := &profile.Profile{MutationBaseline: "mutation-baseline.json"}
	if b, err := LoadBaseline(root, p); b != nil || err != nil {
		t.Errorf("not written yet: %+v, %v", b, err)
	}
	path := filepath.Join(root, "mutation-baseline.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":"0.1.0","scopes":[{"path":"src/**","score":72.5}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := LoadBaseline(root, p); err != nil || len(b.Scopes) != 1 || b.Scopes[0].Score != 72.5 {
		t.Errorf("written: %+v, %v", b, err)
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":"0.1.0","scopes":[{"path":"src/**"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBaseline(root, p); err == nil || !strings.Contains(err.Error(), "mutation-baseline.json") {
		t.Errorf("invalid file: err = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBaseline(root, p); err == nil {
		t.Error("an unreadable baseline file read as none")
	}
}

// Proposing applies the score rule too: a confirmed equivalent of this
// command leaves the scope's count; a pending one, another kind, or
// another command's does not (mutation-threshold.md).
func TestProposeLeavesConfirmedEquivalentsOut(t *testing.T) {
	files := map[string]report.FileCounts{"src/a.go": {Killed: 3, Survived: 12}}
	var fs []report.Finding
	add := func(n int, kind, status, command string) {
		for i := range n {
			f := survivor(fmt.Sprintf("%s-%s-%s-%d", kind, status, command, i), fmt.Sprintf("src/a.go:%d:1", len(fs)+1), kind, status)
			f.Command = command
			fs = append(fs, f)
		}
	}
	add(1, report.KindEquivalentMutant, report.StatusAcknowledged, "mutation_changed")
	add(2, report.KindEquivalentMutant, report.StatusOpen, "mutation_changed")
	add(3, report.KindRequirementSilent, report.StatusAcknowledged, "mutation_changed")
	add(4, report.KindEquivalentMutant, report.StatusAcknowledged, "mutation_full")
	r := mutationReport(report.MutantCounts{Killed: 3, Survived: 12}, files, fs...)
	next, _, err := Propose(nil, r, "mutation_changed", []string{"src/**"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s := next.Scopes[0]; s.Killed != 3 || s.Counted != 14 {
		t.Errorf("proposed %+v, want 3 of 14: only this command's one confirmed equivalent leaves", s)
	}
}

// The ratchet refuses to lower a score; recording the same score again is
// not lowering it (mutation-threshold.md).
func TestChangeLower(t *testing.T) {
	old := 75.0
	for _, tt := range []struct {
		old  *float64
		new  float64
		want bool
	}{{&old, 74.9, true}, {&old, 75, false}, {&old, 80, false}, {nil, 10, false}} {
		if got := (Change{Old: tt.old, New: Scope{Score: tt.new}}).Lower(); got != tt.want {
			t.Errorf("old %v new %v: Lower = %v", tt.old, tt.new, got)
		}
	}
}

// Propose prints the file it would write (mutation-threshold.md): what
// Marshal renders reads back as the same baseline.
func TestMarshalRoundTrips(t *testing.T) {
	b := &Baseline{SchemaVersion: BaselineVersion, Scopes: []Scope{{Path: "src/**", Score: 72.5, Killed: 29, Counted: 40, Command: "mutation_full"}}}
	raw, err := b.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseBaseline(raw)
	if err != nil || len(got.Scopes) != 1 || got.Scopes[0] != b.Scopes[0] {
		t.Errorf("round trip: %+v, %v from %s", got, err, raw)
	}
	if _, err := (&Baseline{SchemaVersion: BaselineVersion, Scopes: []Scope{{Path: "src/**", Score: 101}}}).Marshal(); err == nil {
		t.Error("an invalid baseline was rendered")
	}
}

// CheckOf finds the check a shortfall was made for, so route can name the
// survivors it counts (mutation-threshold.md): the right command, and the
// scope rather than the threshold.
func TestCheckOf(t *testing.T) {
	files := map[string]report.FileCounts{"src/a/x.go": {Killed: 1, Survived: 1}}
	r := mutationReport(report.MutantCounts{Killed: 1, Survived: 1}, files)
	full := r.Run.Commands[0]
	full.ID = "mutation_full"
	r.Run.Commands = append(r.Run.Commands, full)
	p := &profile.Profile{Commands: map[string]profile.Command{"mutation_changed": {Run: "x"}, "mutation_full": {Run: "x"}}}
	Apply(r, p, &Baseline{Scopes: []Scope{{Path: "src/a/**", Score: 90}}})
	var short report.Finding
	for _, f := range shortfalls(r) {
		if f.Command == "mutation_full" {
			short = f
		}
	}
	ch, ok := CheckOf(r, short)
	if !ok || ch.Command != "mutation_full" || ch.Scope != "src/a/**" {
		t.Fatalf("CheckOf = %+v, %v; want mutation_full's src/a/** check", ch, ok)
	}
	if _, ok := CheckOf(r, report.Finding{ID: "F-mutation-full-0", Command: "mutation_full", Kind: report.KindBelowThreshold}); ok {
		t.Error("a shortfall no check made was found")
	}
}

// mutation-threshold.md, the owner's call of 2026-10-19: a shortfall names
// at most 10 survivors, then "and N more"; the survivor findings hold the
// full list.
func TestShortfallNamesTenSurvivorsThenCounts(t *testing.T) {
	for _, n := range []int{10, 11, 12} {
		var fs []report.Finding
		for i := range n {
			fs = append(fs, survivor(fmt.Sprintf("s%02d", i), "src/a.go:1:1", report.KindUnclassified, report.StatusOpen))
		}
		r := mutationReport(report.MutantCounts{Killed: 1, Survived: n}, nil, fs...)
		Apply(r, threshold(100), nil)
		msg := shortfalls(r)[0].Message
		var named []string
		for i := range n {
			if strings.Contains(msg, fmt.Sprintf("F-mutation-changed-s%02d [", i)) {
				named = append(named, fmt.Sprint(i))
			}
		}
		more := ""
		if n > 10 {
			more = fmt.Sprintf(" and %d more", n-10)
		}
		if len(named) != 10 || named[9] != "9" || !strings.HasSuffix(msg, "F-mutation-changed-s09 [unclassified]"+more) {
			t.Errorf("%d survivors: named %v; message:\n%s", n, named, msg)
		}
	}
}
