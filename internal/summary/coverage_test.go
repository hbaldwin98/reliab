package summary

import (
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

// Coverage is reported as counts, and counts measured on changed lines say
// so, for mutants as well.
func TestMarkdownCoverageAndChangedCounts(t *testing.T) {
	s := &Summary{
		Tier: "prepr", Complete: true, Fresh: true,
		Ran:      []Ran{{ID: "coverage", Coverage: &report.LineCounts{Valid: 8, Covered: 6, ChangedSince: "main"}}},
		Mutation: []Mutation{{Command: "mutation_changed", Mutants: &report.MutantCounts{Killed: 4, Survived: 2, ChangedSince: "main"}}},
		Skipped:  []report.Skip{{ID: "unit", Reason: report.SkipBuildFailed}},
	}
	md := Markdown(s)
	// No coverage command, no section; a report that measured no lines
	// says 0% rather than dividing by zero.
	if md := Markdown(&Summary{Tier: "prepr", Complete: true, Fresh: true, Ran: []Ran{{ID: "unit"}}}); strings.Contains(md, "#### Coverage") {
		t.Errorf("coverage section without a coverage command:\n%s", md)
	}
	empty := Markdown(&Summary{Tier: "prepr", Complete: true, Fresh: true, Ran: []Ran{{ID: "coverage", Coverage: &report.LineCounts{}}}})
	if !strings.Contains(empty, "- `coverage`: 0 of 0 lines covered (0.0%).") {
		t.Errorf("empty coverage:\n%s", empty)
	}
	for _, want := range []string{
		"#### Coverage",
		"- `coverage`: 6 of 8 lines covered (75.0%), lines changed since `main` only.",
		"- `mutation_changed`: 6 tested, 4 killed, 2 survived (lines changed since `main` only).",
		"| unit | the build failed |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}
