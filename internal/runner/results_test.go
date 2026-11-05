package runner

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/report"
)

// Source for every test in this file: README "Run": one line per command,
// then each failure's findings and its about; a blocked command names what
// did not pass; exit 1 when something failed.

func TestPrintResultsSummaryAndFailures(t *testing.T) {
	results := []*StepResult{
		{Step: &Step{Name: "build"}, Status: Passed, Elapsed: 1300 * time.Millisecond},
		{Step: &Step{Name: "unit", About: "Unit tests.\nFix the code."}, Status: Failed, Elapsed: 41200 * time.Millisecond,
			Log: ".reliab/run/unit.log", Outside: 2,
			Findings: []report.Finding{{Test: "UsersTests.GetUser", Location: "src/Users.cs:42", Message: "Expected 404, got 500"}}},
		{Step: &Step{Name: "mutation"}, Status: Blocked, BlockedBy: "unit"},
	}
	var b bytes.Buffer
	if !PrintResults(&b, results) {
		t.Fatal("a failed step must report failure")
	}
	want := `
pass     build        1.3s
FAIL     unit         41.2s  (2 findings outside the target)
blocked  mutation     unit did not pass

unit: failed (log: .reliab/run/unit.log)
  - UsersTests.GetUser src/Users.cs:42
    Expected 404, got 500
  about: Unit tests. Fix the code.
`
	if b.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestPrintResultsAllPassed(t *testing.T) {
	var b bytes.Buffer
	if PrintResults(&b, []*StepResult{{Step: &Step{Name: "build"}, Status: Passed}}) {
		t.Fatal("all passed must not report failure")
	}
	if strings.Contains(b.String(), "about:") {
		t.Fatalf("no failure details when everything passed:\n%s", b.String())
	}
}

func TestPrintResultsEveryNonPassFails(t *testing.T) {
	for _, s := range []Status{Failed, Blocked, TimedOut, Cancelled} {
		var b bytes.Buffer
		if !PrintResults(&b, []*StepResult{{Step: &Step{Name: "x"}, Status: s, BlockedBy: "y"}}) {
			t.Errorf("%s must count as not passed", s)
		}
	}
	marks := map[Status]string{Passed: "pass", Failed: "FAIL", Blocked: "blocked", TimedOut: "timeout", Cancelled: "cancel"}
	for s, m := range marks {
		if mark(s) != m {
			t.Errorf("mark(%s) = %q, want %q", s, mark(s), m)
		}
	}
}

func TestPrintResultsFindingShapes(t *testing.T) {
	// A message that already names the location does not repeat it; a
	// finding with no location shows its message; long messages are cut.
	long := strings.Repeat("line\n", 10)
	r := &StepResult{Step: &Step{Name: "lint"}, Status: Failed, Log: "l", Findings: []report.Finding{
		{Location: "a.go:3", Message: "errcheck: unchecked a.go:3"},
		{Message: "exit 2\nboom"},
		{Test: "T", Message: long},
	}}
	var b bytes.Buffer
	PrintResults(&b, []*StepResult{r})
	out := b.String()
	if strings.Count(out, "a.go:3") != 1 {
		t.Errorf("location repeated:\n%s", out)
	}
	if !strings.Contains(out, "  - exit 2\n    boom\n") {
		t.Errorf("message-only finding:\n%s", out)
	}
	if strings.Count(out, "line") != 6 || !strings.Contains(out, "    ...\n") {
		t.Errorf("long message not cut to 6 lines:\n%s", out)
	}
}

func TestPrintResultsCapsFindings(t *testing.T) {
	r := &StepResult{Step: &Step{Name: "lint"}, Status: Failed}
	for i := range 13 {
		r.Findings = append(r.Findings, report.Finding{Location: fmt.Sprintf("f%d.go:1", i), Message: "m"})
	}
	var b bytes.Buffer
	PrintResults(&b, []*StepResult{r})
	if !strings.Contains(b.String(), "... and 3 more") || strings.Contains(b.String(), "f10.go") {
		t.Fatalf("want 10 findings then a count:\n%s", b.String())
	}
}
