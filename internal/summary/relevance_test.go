package summary

import (
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

// R8: a relevance skip is reported with its reason, and a command with
// relevance rules says why it ran (knowledge/decisions/relevance-skips.md).
func TestMarkdownRelevance(t *testing.T) {
	s := &Summary{
		Tier: "prepr", Complete: true, Fresh: true,
		Ran:     []Ran{{ID: "unit"}, {ID: "api_fuzz", Relevance: "src/api/routes.go changed since main and matches src/api/**"}},
		Skipped: []report.Skip{{ID: "faults_db", Reason: report.SkipNotRelevant, Detail: "the change since main touches only README.md; it is relevant only to src/data/**"}},
	}
	md := Markdown(s)
	for _, want := range []string{
		"| faults_db | not relevant to the change | the change since main touches only README.md; it is relevant only to src/data/** |",
		"Why commands with relevance rules ran:\n\n- `api_fuzz`: src/api/routes.go changed since main and matches src/api/**.\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if md := Markdown(&Summary{Tier: "prepr", Complete: true, Fresh: true, Ran: []Ran{{ID: "unit"}}}); strings.Contains(md, "relevance rules") {
		t.Errorf("relevance lines with no relevance rule:\n%s", md)
	}
}
