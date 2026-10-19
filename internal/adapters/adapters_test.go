package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

const junitNested = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="pkg">
    <testcase classname="store::tests" name="passes"/>
    <testcase classname="store::tests" name="reconcile" file="src/store.rs" line="42">
      <failure message="assertion failed" type="panic">left: 1, right: 2</failure>
    </testcase>
    <testsuite name="nested">
      <testcase name="errors"><error message="timeout"/></testcase>
    </testsuite>
  </testsuite>
</testsuites>`

func TestParseJUnit(t *testing.T) {
	got, err := ParseJUnit([]byte(junitNested))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("failures = %+v, want 2", got)
	}
	if got[0].Test != "store::tests::reconcile" || got[0].Location != "src/store.rs:42" {
		t.Errorf("first = %+v", got[0])
	}
	if !strings.Contains(got[0].Message, "left: 1, right: 2") || !strings.Contains(got[0].Message, "panic") {
		t.Errorf("message = %q", got[0].Message)
	}
	if got[1].Test != "errors" || !strings.Contains(got[1].Message, "timeout") {
		t.Errorf("second = %+v", got[1])
	}
}

func TestParseJUnitSingleSuiteRoot(t *testing.T) {
	got, err := ParseJUnit([]byte(`<testsuite><testcase name="x"><failure>boom</failure></testcase></testsuite>`))
	if err != nil || len(got) != 1 || got[0].Test != "x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

const trx = `<?xml version="1.0" encoding="utf-8"?>
<TestRun xmlns="http://microsoft.com/schemas/VisualStudio/TeamTest/2010">
  <Results>
    <UnitTestResult testName="Parser_RejectsEmptyInput" outcome="Failed">
      <Output><ErrorInfo><Message>Falsifiable after 3 tests</Message><StackTrace>at Run()</StackTrace></ErrorInfo></Output>
    </UnitTestResult>
    <UnitTestResult testName="Passes" outcome="Passed"/>
  </Results>
</TestRun>`

func TestParseTRX(t *testing.T) {
	got, err := ParseTRX([]byte(trx))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Test != "Parser_RejectsEmptyInput" || !strings.Contains(got[0].Message, "Falsifiable") {
		t.Fatalf("got %+v", got)
	}
}

func TestFindings(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "junit.xml")
	if err := os.WriteFile(out, []byte(junitNested), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.xml")

	tests := []struct {
		name, format, output string
		exit                 int
		want                 int
		wantID               string
	}{
		{"junit failures", "junit", out, 1, 2, ""},
		{"none and pass", "none", "", 0, 0, ""},
		{"none and fail", "none", "", 2, 1, "F-unit"},
		{"junit output missing on failure", "junit", missing, 101, 1, "F-unit"},
		{"junit output missing on success", "junit", missing, 0, 1, "F-unit"},
		{"unparsed format falls back to exit code", "sarif", out, 0, 0, ""},
		{"unparsed format with its output missing", "sarif", missing, 0, 1, "F-unit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findingsFor("unit", tt.format, tt.output, tt.exit)
			if len(got) != tt.want {
				t.Fatalf("findings = %+v, want %d", got, tt.want)
			}
			for _, f := range got {
				if f.Kind != "unclassified" || f.Status != "open" || f.Command != "unit" {
					t.Errorf("finding not unclassified/open/unit: %+v", f)
				}
			}
			if tt.wantID != "" && got[0].ID != tt.wantID {
				t.Errorf("id = %q, want %q", got[0].ID, tt.wantID)
			}
		})
	}
}

// findingsFor runs Findings the way verify does for one output path:
// declared when output is set, present when the file exists.
func findingsFor(command, format, output string, exit int) []report.Finding {
	return resultFor(command, format, output, exit).Findings
}

func resultFor(command, format, output string, exit int) Result {
	in := Input{Command: command, Format: format, Display: output, Declared: output != "", ExitCode: exit, LogTail: "tail of log"}
	if _, err := os.Stat(output); output != "" && err == nil {
		in.Outputs = []string{output}
	}
	return Findings(in)
}

func TestIDStable(t *testing.T) {
	a, b := ID("mutation_changed", "x::y"), ID("mutation_changed", "x::y")
	if a != b || !strings.HasPrefix(a, "F-mutation-changed-") {
		t.Fatalf("ids %q %q", a, b)
	}
	if ID("unit", "x") == ID("unit", "y") {
		t.Fatal("different tests share an id")
	}
}
