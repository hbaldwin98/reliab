package summary

import (
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

// The summary shows each mutation score against its threshold and
// baselines, with the equivalents it left out or still counts
// (knowledge/decisions/mutation-threshold.md).
func TestMarkdownScoreAgainstThresholdAndBaseline(t *testing.T) {
	eighty := 80.0
	s := &Summary{
		Tier: "prepr", Complete: true, Fresh: true,
		Mutation: []Mutation{{
			Command: "mutation_changed",
			Mutants: &report.MutantCounts{Killed: 7, Survived: 3},
			Score: &report.Score{
				Killed: 7, Counted: 9, Equivalent: 1, PendingEquivalent: 1, Threshold: &eighty,
				Baselines: []report.ScopeScore{
					{Scope: "src/billing/**", Baseline: 60, Killed: 3, Counted: 4},
					{Scope: "src/cache/**", Baseline: 72.5, Unmeasured: true},
				},
			},
		}, {
			Command: "mutation_full",
			Mutants: &report.MutantCounts{},
			Score:   &report.Score{Threshold: &eighty},
		}},
	}
	md := Markdown(s)
	for _, want := range []string{
		"  - Score 77.8% (7 killed of 9 counted), less 1 survivor(s) a person confirmed equivalent; 1 classified equivalent and waiting for a person, still counted. Threshold 80%: **below**.",
		"  - Baseline `src/billing/**` 60%: 75.0% (3 killed of 4 counted) here, met.",
		"  - Baseline `src/cache/**` 72.5%: not scored, the tool reported no per-file counts.",
		"  - Score not defined: no mutants counted. Threshold 80%: nothing to hold.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if md := Markdown(&Summary{Tier: "prepr", Complete: true, Fresh: true, Mutation: []Mutation{{Command: "mutation_changed"}}}); strings.Contains(md, "Score") {
		t.Errorf("a score line without a score:\n%s", md)
	}
}

// A shortfall is a question for a person, and is not listed as a survivor.
func TestShortfallIsAQuestionNotASurvivor(t *testing.T) {
	short := report.Finding{ID: "F-mutation-changed-1", Kind: report.KindBelowThreshold, Command: "mutation_changed", Test: "mutation score 70.0% below the threshold 80% (7 killed of 10 counted)", Status: report.StatusOpen}
	if asks[report.KindBelowThreshold] == "" {
		t.Fatal("no question for a shortfall")
	}
	if !report.NeedsHuman(short) || report.NeedsAgent(short) {
		t.Error("a shortfall must wait for a person and never block the agent's stop: its survivors already do")
	}
}
