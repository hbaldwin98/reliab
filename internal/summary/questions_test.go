package summary

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/approval"
	"github.com/hbaldwin98/reliab/internal/gate"
	"github.com/hbaldwin98/reliab/internal/report"
)

// The JSON's questions and the Markdown's "Questions for a human" list the
// same entries: a reviewer reading the JSON once saw no questions while the
// Markdown listed an unapproved protected change
// (knowledge/decisions/pr-evidence-summary.md).
func TestJSONQuestionsMatchMarkdown(t *testing.T) {
	s := &Summary{Tier: "prepr", Base: "main", Ran: []Ran{}, Open: []Item{}, Acknowledged: []Item{}, Fixed: []Item{},
		Questions: []Item{{ID: "F-unit", Kind: report.KindEnvironment, Command: "unit", Ask: asks[report.KindEnvironment]}},
		Protected: []gate.ProtectedChange{
			{Path: "testdata/seeds/x", Glob: "testdata/**"},
			{Path: "snapshots/a.snap", Glob: "snapshots/**", Approval: &approval.Approval{By: "human:owner"}},
			{Path: "reliability-profile.yaml", Glob: "reliability-profile.yaml"},
		},
	}
	var buf bytes.Buffer
	if err := Write(&buf, s, "json"); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Questions []Item `json:"questions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	md := Markdown(s)
	section := md[strings.Index(md, "#### Questions for a human"):]
	numbered := regexp.MustCompile(`(?m)^\d+\. `).FindAllString(section, -1)
	if len(doc.Questions) != 3 || len(numbered) != len(doc.Questions) {
		t.Fatalf("json questions %+v; markdown numbers %d:\n%s", doc.Questions, len(numbered), section)
	}
	for i, want := range []string{"`F-unit`", "`testdata/seeds/x`", "`reliability-profile.yaml`"} {
		q := doc.Questions[i]
		if !strings.Contains("`"+q.ID+"`"+"`"+q.Subject+"`", want) || !strings.Contains(section, want) {
			t.Errorf("question %d = %+v, want %s in both", i, q, want)
		}
	}
	if q := doc.Questions[1]; q.Kind != QuestionProtectedChange || q.Glob != "testdata/**" || !strings.Contains(q.Ask, "reliab approve testdata/seeds/x") {
		t.Errorf("protected question = %+v", q)
	}

	// Protected paths that could not be checked are a question in both.
	s = &Summary{Tier: "prepr", Questions: []Item{}, Protected: []gate.ProtectedChange{}, ProtectedError: "no base ref\nfound"}
	buf.Reset()
	if err := Write(&buf, s, "json"); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Questions) != 1 || doc.Questions[0].Kind != QuestionProtectedUnchecked || doc.Questions[0].Reason != "no base ref found" ||
		!strings.Contains(Markdown(s), "- Protected paths not checked: no base ref found.") {
		t.Errorf("unchecked: %+v", doc.Questions)
	}
	// Nothing waits: both say so.
	s = &Summary{Tier: "prepr", Questions: []Item{}, Protected: []gate.ProtectedChange{}}
	buf.Reset()
	if err := Write(&buf, s, "json"); err != nil || !strings.Contains(buf.String(), `"questions": []`) || !strings.Contains(Markdown(s), "None: no open finding") {
		t.Errorf("none: %v %s", err, buf.String())
	}
}
