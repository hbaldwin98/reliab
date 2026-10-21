package repocheck

import (
	"regexp"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

// section returns the Markdown under heading, up to the next heading of the
// same or a higher level.
func section(t *testing.T, path, heading string) string {
	t.Helper()
	doc := readFile(t, path)
	level := strings.Index(heading, " ")
	start := strings.Index(doc, "\n"+heading+"\n")
	if start < 0 {
		t.Fatalf("%s: no %q heading", path, heading)
	}
	body := doc[start+len(heading)+2:]
	end := regexp.MustCompile(`(?m)^#{1,` + string(rune('0'+level)) + `} `).FindStringIndex(body)
	if end != nil {
		body = body[:end[0]]
	}
	return body
}

func TestFindingKindConceptTablesEveryKind(t *testing.T) {
	path := "knowledge/concepts/finding-kind.md"
	rows := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\| ([a-z-]+) \| ([^|]+) \|`).FindAllStringSubmatch(readFile(t, path), -1) {
		rows[m[1]] = strings.TrimSpace(m[2])
	}
	for _, p := range report.Kinds {
		actor, ok := rows[p.Kind]
		if !ok {
			t.Errorf("%s: no row for %s", path, p.Kind)
			continue
		}
		if want := map[bool]string{true: "human", false: "agent"}[p.Human]; !strings.HasPrefix(actor, want) {
			t.Errorf("%s: %s is resolved by %q, but report.Kinds says %s", path, p.Kind, actor, want)
		}
		delete(rows, p.Kind)
	}
	for k := range rows {
		t.Errorf("%s: row %s is not in report.Kinds", path, k)
	}
}

// The kinds reliab added to the paper's are each recorded in the decision.
func TestReliabKindsAreInTheDecision(t *testing.T) {
	concept := readFile(t, "knowledge/concepts/finding-kind.md")
	decision := readFile(t, "knowledge/decisions/finding-classification.md")
	added := regexp.MustCompile(`(?m)^\| ([a-z-]+) \|.*reliab-specific`).FindAllStringSubmatch(concept, -1)
	if len(added) == 0 {
		t.Fatal("finding-kind.md marks no kind reliab-specific")
	}
	for _, m := range added {
		if !strings.Contains(decision, "`"+m[1]+"`") {
			t.Errorf("finding-classification.md does not record %s", m[1])
		}
	}
}

func TestReportSchemaEnumListsEveryKind(t *testing.T) {
	var s struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	readJSON(t, "schemas/report.schema.json", &s)
	var enum []string
	for _, d := range s.Defs {
		if k, ok := d.Properties["kind"]; ok && len(k.Enum) > 0 {
			enum = k.Enum
		}
	}
	if !sameSet(enum, kindNames()) {
		t.Errorf("report.schema.json kind enum %v, report.Kinds %v", enum, kindNames())
	}
}

func TestSkillsListEveryKind(t *testing.T) {
	for _, c := range []struct{ path, heading string }{
		{"plugin/skills/evidence-stack/SKILL.md", "## Finding kinds"},
		{"plugin/skills/verify/SKILL.md", "## Then"},
	} {
		body := section(t, c.path, c.heading)
		for _, k := range kindNames() {
			if !strings.Contains(body, "`"+k+"`") {
				t.Errorf("%s %q does not name %s", c.path, c.heading, k)
			}
		}
	}
	// The verify skill's stop bullet names exactly the kinds that stop.
	var stop string
	for _, b := range strings.Split(section(t, "plugin/skills/verify/SKILL.md", "## Then"), "\n   - ") {
		if strings.Contains(b, "stop and tell the user") {
			stop = b
		}
	}
	if stop == "" {
		t.Fatal("verify skill: no stop bullet")
	}
	for _, p := range report.Kinds {
		if named := strings.Contains(stop, "`"+p.Kind+"`"); named != p.Human {
			t.Errorf("verify skill stop bullet names %s: %v, but it stops for a person: %v", p.Kind, named, p.Human)
		}
	}
}

// AGENTS.md and the template it is written from list every kind an agent
// can meet in a report. unclassified is the exception: every finding starts
// so, and the "Always" rules say to reproduce and classify it.
func TestAgentRulesListEveryKind(t *testing.T) {
	for _, path := range []string{"templates/agents.md", "AGENTS.md"} {
		body := section(t, path, "### When a test fails")
		for _, k := range kindNames() {
			if k == report.KindUnclassified {
				continue
			}
			if !strings.Contains(body, "\n- "+k+" -> ") && !strings.HasPrefix(body, "- "+k+" -> ") {
				t.Errorf("%s \"When a test fails\" has no line for %s", path, k)
			}
		}
	}
}

func kindNames() []string {
	out := make([]string, len(report.Kinds))
	for i, p := range report.Kinds {
		out[i] = p.Kind
	}
	return out
}

func sameSet(a, b []string) bool {
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return len(a) == len(b)
}
