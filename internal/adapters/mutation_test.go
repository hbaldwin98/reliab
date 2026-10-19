package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

const strykerJSON = `{"schemaVersion":"2","thresholds":{"high":80,"low":60},"files":{"src/Parser.cs":{"language":"cs","source":"","mutants":[
  {"id":"1","mutatorName":"EqualityOperator","replacement":"<=","status":"Survived","location":{"start":{"line":10,"column":5},"end":{"line":10,"column":6}}},
  {"id":"2","mutatorName":"BlockStatement","status":"NoCoverage","location":{"start":{"line":20,"column":1},"end":{"line":22,"column":1}}},
  {"id":"3","mutatorName":"StringLiteral","status":"Killed","location":{"start":{"line":30,"column":1},"end":{"line":30,"column":9}}},
  {"id":"4","mutatorName":"Logical","status":"Timeout","location":{"start":{"line":40,"column":1},"end":{"line":40,"column":9}}}]}}}`

func TestParseMutationReport(t *testing.T) {
	got, err := ParseMutationReport([]byte(strykerJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("failures = %+v, want survived and no coverage", got)
	}
	if got[0].Test != "src/Parser.cs: EqualityOperator: replace with <=" || got[0].Location != "src/Parser.cs:10:5" {
		t.Errorf("first = %+v", got[0])
	}
	if !strings.Contains(got[1].Message, "no test covers") {
		t.Errorf("second = %+v", got[1])
	}
}

func TestParseMutationReportKeyIgnoresLine(t *testing.T) {
	moved := strings.Replace(strykerJSON, `"line":10`, `"line":15`, 1)
	a, _ := ParseMutationReport([]byte(strykerJSON))
	b, _ := ParseMutationReport([]byte(moved))
	if a[0].Test != b[0].Test || a[0].Location == b[0].Location {
		t.Fatalf("key should survive a line shift: %+v / %+v", a[0], b[0])
	}
}

const findingsJSON = `{"schemaVersion":"0.1.0","findings":[
  {"test":"src/store.rs: replace < with <= in migrate","location":"src/store.rs:227:17","message":"surviving mutant"},
  {"test":"prop_roundtrip","replay":"proptest-regressions/x.txt","observed":"1","expected":"2"}]}`

func TestParseFindings(t *testing.T) {
	got, err := ParseFindings([]byte(findingsJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Location != "src/store.rs:227:17" || got[1].Replay != "proptest-regressions/x.txt" || got[1].Expected != "2" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseFindingsRejects(t *testing.T) {
	for name, body := range map[string]string{
		"not json":        `nope`,
		"no version":      `{"findings":[]}`,
		"other major":     `{"schemaVersion":"1.0.0","findings":[]}`,
		"missing test":    `{"schemaVersion":"0.1.0","findings":[{"message":"x"}]}`,
		"unknown field":   `{"schemaVersion":"0.1.0","findings":[{"test":"a","kind":"new-code-defect"}]}`,
		"duplicate tests": `{"schemaVersion":"0.1.0","findings":[{"test":"a"},{"test":"a"}]}`,
	} {
		if _, err := ParseFindings([]byte(body)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestDecisiveFindings(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	two := write("two.json", findingsJSON)
	none := write("none.json", `{"schemaVersion":"0.1.0","findings":[]}`)
	bad := write("bad.json", `{"findings":[]}`)
	missing := filepath.Join(dir, "missing.json")

	tests := []struct {
		name, output string
		exit, want   int
		wantID       string
	}{
		{"findings, exit 0", two, 0, 2, ""},
		{"findings, exit 2", two, 2, 2, ""},
		// The adapter decided the tool's nonzero exit was not a failure.
		{"no findings, exit 3", none, 3, 0, ""},
		{"invalid file", bad, 0, 1, "F-mutation-changed"},
		{"no file, exit 0", missing, 0, 1, "F-mutation-changed"},
		{"no file, exit 1", missing, 1, 1, "F-mutation-changed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findingsFor("mutation_changed", "findings", tt.output, tt.exit)
			if len(got) != tt.want {
				t.Fatalf("findings = %+v, want %d", got, tt.want)
			}
			if tt.wantID != "" && got[0].ID != tt.wantID {
				t.Errorf("id = %q, want %q", got[0].ID, tt.wantID)
			}
			for _, f := range got {
				if f.Kind != "unclassified" || f.Status != "open" {
					t.Errorf("finding = %+v", f)
				}
			}
		})
	}
	if got := findingsFor("mutation_changed", "findings", two, 0); got[1].Replay == "" || got[1].Observed != "1" {
		t.Errorf("replay/observed not carried: %+v", got[1])
	}
}

// A report says how many mutants were tested, not only which survived
// (knowledge/decisions/pr-evidence-summary.md): the PR summary states
// tested, killed, and survived.
func TestCounts(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stryker := write("stryker.json", strykerJSON)
	if c := resultFor("mutation_full", "mutation-report", stryker, 0).Mutants; c == nil || *c != (report.MutantCounts{Killed: 2, Survived: 1, NotCovered: 1}) {
		t.Errorf("mutation-report counts = %+v, want 2 killed (one a timeout), 1 survived, 1 not covered", c)
	}
	counted := write("counted.json", `{"schemaVersion":"0.3.0","mutants":{"killed":7,"survived":1},"findings":[{"test":"a.go: CONDITIONALS_BOUNDARY"}]}`)
	if c := resultFor("mutation_full", "findings", counted, 0).Mutants; c == nil || *c != (report.MutantCounts{Killed: 7, Survived: 1}) {
		t.Errorf("findings counts = %+v", c)
	}
	for name, path := range map[string]string{
		"findings without counts": write("plain.json", findingsJSON),
		"invalid findings file":   write("bad.json", `{"schemaVersion":"0.3.0","mutants":{"killed":-1,"survived":0},"findings":[]}`),
		"missing file":            filepath.Join(dir, "missing.json"),
	} {
		if c := resultFor("mutation_full", "findings", path, 0).Mutants; c != nil {
			t.Errorf("%s: counts = %+v, want none", name, c)
		}
	}
	if c := resultFor("mutation_full", "junit", stryker, 0).Mutants; c != nil {
		t.Errorf("junit has no counts: %+v", c)
	}
}

// Counts come per file too, so a baseline scope can be scored
// (knowledge/decisions/mutation-threshold.md).
func TestFileCounts(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stryker := write("stryker.json", strykerJSON)
	files := resultFor("mutation_full", "mutation-report", stryker, 0).MutantFiles
	if len(files) != 1 || files["src/Parser.cs"] != (report.FileCounts{Killed: 2, Survived: 1, NotCovered: 1}) {
		t.Errorf("mutation-report files = %+v", files)
	}
	a := write("a.json", `{"schemaVersion":"0.4.0","mutants":{"killed":3,"survived":1},"mutantFiles":{"a.go":{"killed":2,"survived":1},"b.go":{"killed":1,"survived":0}},"findings":[]}`)
	files = resultFor("mutation_full", "findings", a, 0).MutantFiles
	if len(files) != 2 || files["a.go"] != (report.FileCounts{Killed: 2, Survived: 1}) || files["b.go"] != (report.FileCounts{Killed: 1}) {
		t.Errorf("findings files = %+v", files)
	}
	// Two result files that count the same file add up.
	in := Input{Command: "mutation_full", Format: "findings", Display: "*.json", Declared: true, Outputs: []string{a, a}}
	if got := Findings(in).MutantFiles["a.go"]; got != (report.FileCounts{Killed: 4, Survived: 2}) {
		t.Errorf("merged a.go = %+v", got)
	}
	if files := resultFor("mutation_full", "findings", write("old.json", `{"schemaVersion":"0.3.0","mutants":{"killed":1,"survived":0},"findings":[]}`), 0).MutantFiles; files != nil {
		t.Errorf("a file without per-file counts: %+v", files)
	}
}
