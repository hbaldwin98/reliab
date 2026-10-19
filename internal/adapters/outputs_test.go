package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

// A TRX result names the bare method; its test definition gives the class.
// Two classes with a same-named test failing must not share a finding id.
const trxTwoClasses = `<?xml version="1.0" encoding="utf-8"?>
<TestRun xmlns="http://microsoft.com/schemas/VisualStudio/TeamTest/2010">
  <TestDefinitions>
    <UnitTest name="Rejects" id="a1"><TestMethod className="Shop.Tests.DupA" name="Rejects"/></UnitTest>
    <UnitTest name="Rejects" id="b1"><TestMethod className="Shop.Tests.DupB" name="Rejects"/></UnitTest>
    <UnitTest name="Shop.Tests.Full.Name" id="c1"><TestMethod className="Shop.Tests.Full" name="Name"/></UnitTest>
  </TestDefinitions>
  <Results>
    <UnitTestResult testId="a1" testName="Rejects" outcome="Failed"><Output><ErrorInfo><Message>A</Message></ErrorInfo></Output></UnitTestResult>
    <UnitTestResult testId="b1" testName="Rejects" outcome="Failed"><Output><ErrorInfo><Message>B</Message></ErrorInfo></Output></UnitTestResult>
    <UnitTestResult testId="c1" testName="Shop.Tests.Full.Name" outcome="Failed"/>
  </Results>
</TestRun>`

func TestParseTRXQualifiesByClass(t *testing.T) {
	got, err := ParseTRX([]byte(trxTwoClasses))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Shop.Tests.DupA.Rejects", "Shop.Tests.DupB.Rejects", "Shop.Tests.Full.Name"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		if got[i].Test != w {
			t.Errorf("test %d = %q, want %q", i, got[i].Test, w)
		}
	}
}

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Several result files (one per test project) are merged, and a test name
// reported twice is numbered so each finding keeps its own id.
func TestFindingsMergesOutputsAndNumbersDuplicates(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.trx", trx)
	b := write(t, dir, "b.trx", trx)
	got := Findings(Input{Command: "unit", Format: "trx", Outputs: []string{a, b}, Declared: true, Display: "*.trx", ExitCode: 1}).Findings
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want one per file", got)
	}
	if got[0].Test != "Parser_RejectsEmptyInput" || got[1].Test != "Parser_RejectsEmptyInput (#2)" || got[0].ID == got[1].ID {
		t.Errorf("tests %q %q ids %q %q", got[0].Test, got[1].Test, got[0].ID, got[1].ID)
	}
}

func TestRelLocation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	in := filepath.Join(root, "src", "a.cs")
	for loc, want := range map[string]string{
		in + ":12:5": "src/a.cs:12:5",
		// Tools on Windows also write drive paths with forward slashes.
		filepath.ToSlash(in) + ":12:5": "src/a.cs:12:5",
		// A location may end before its line.
		"a:":            "a:",
		"src/b.cs:3":    "src/b.cs:3",
		"/elsewhere:1":  "/elsewhere:1",
		"":              "",
		in:              "src/a.cs",
		"../out.cs:9:1": "../out.cs:9:1",
	} {
		if got := relLocation(root, loc); got != want {
			t.Errorf("relLocation(%q) = %q, want %q", loc, got, want)
		}
	}
}

// Tools write absolute or projectRoot-relative paths; findings use paths
// relative to the repository, so a mutant keeps its id on any checkout.
// The mutated line is shown as a diff built from the report's source.
func TestMutationReportPathsAndDiff(t *testing.T) {
	root := t.TempDir()
	src := "class A {\n  bool Ok(int q) => q > 0;\n}\n"
	abs := filepath.ToSlash(filepath.Join(root, "src", "A.cs"))
	body := `{"schemaVersion":"2","projectRoot":` + jsonString(filepath.Join(root, "src")) + `,"files":{
	  ` + jsonString(abs) + `:{"source":` + jsonString(src) + `,"mutants":[
	    {"mutatorName":"Equality mutation","replacement":"q >= 0","status":"Survived","location":{"start":{"line":2,"column":21},"end":{"line":2,"column":26}}}]},
	  "B.cs":{"source":"","mutants":[
	    {"mutatorName":"Statement mutation","replacement":";","status":"NoCoverage","location":{"start":{"line":1,"column":1},"end":{"line":1,"column":2}}}]}}}`
	out := write(t, root, "report.json", body)
	got := Findings(Input{Command: "mutation_changed", Format: "mutation-report", Root: root, Outputs: []string{out}, Declared: true}).Findings
	if len(got) != 2 {
		t.Fatalf("findings = %+v", got)
	}
	if got[0].Test != "src/A.cs: Equality mutation: replace with q >= 0" || got[0].Location != "src/A.cs:2:21" {
		t.Errorf("first = %+v", got[0])
	}
	if !strings.Contains(got[0].Message, "-   bool Ok(int q) => q > 0;") || !strings.Contains(got[0].Message, "+   bool Ok(int q) => q >= 0;") {
		t.Errorf("message has no diff of the mutated line: %q", got[0].Message)
	}
	if got[1].Location != "src/B.cs:1:1" {
		t.Errorf("projectRoot-relative key not resolved: %+v", got[1])
	}
}

// With a changed-lines filter only mutants on changed lines are findings,
// and the counts cover only them and say so.
func TestMutationReportChangedOnly(t *testing.T) {
	root := t.TempDir()
	out := write(t, root, "report.json", strykerJSON)
	changed := map[string]map[int]bool{"src/Parser.cs": {10: true, 30: true}}
	res := Findings(Input{Command: "mutation_changed", Format: "mutation-report", Root: root, Outputs: []string{out}, Declared: true, Changed: changed, Base: "main"})
	if len(res.Findings) != 1 || res.Findings[0].Location != "src/Parser.cs:10:5" {
		t.Fatalf("findings = %+v, want the survivor on line 10 only", res.Findings)
	}
	if res.Mutants == nil || *res.Mutants != (report.MutantCounts{Killed: 1, Survived: 1, ChangedSince: "main"}) {
		t.Errorf("counts = %+v", res.Mutants)
	}
}

func TestMutatedLinesOutOfRange(t *testing.T) {
	lines := []string{"a", "bc"}
	for _, c := range []struct{ s, e position }{
		{position{0, 1}, position{1, 1}},
		{position{1, 1}, position{3, 1}},
		{position{2, 5}, position{2, 6}},
		{position{2, 2}, position{2, 1}},
	} {
		if got := mutatedLines(lines, c.s, c.e, "x"); got != "" {
			t.Errorf("%+v: got %q, want empty", c, got)
		}
	}
	if got := mutatedLines(nil, position{1, 1}, position{1, 1}, "x"); got != "" {
		t.Errorf("no source: %q", got)
	}
	// A zero-width span is an insertion; at the end of a line its columns
	// are one past the last character (mutation-testing-elements columns
	// are 1-based, the end exclusive).
	if got := mutatedLines(lines, position{2, 3}, position{2, 3}, "d"); got != "- bc\n+ bcd" {
		t.Errorf("insertion at the end of a line: %q", got)
	}
	// The first and last lines and columns are in range.
	if got := mutatedLines(lines, position{1, 1}, position{1, 2}, "z"); got != "- a\n+ z" {
		t.Errorf("line 1: %q", got)
	}
	if got := mutatedLines(lines, position{2, 2}, position{2, 3}, "x"); got != "- bc\n+ bx" {
		t.Errorf("last line: %q", got)
	}
}

const cobertura = `<?xml version="1.0"?>
<coverage line-rate="0.5">
  <sources><source>SRC</source></sources>
  <packages><package name="p"><classes>
    <class name="A" filename="lib/A.cs"><lines>
      <line number="1" hits="3"/><line number="2" hits="0"/><line number="3" hits="1"/>
    </lines></class>
    <class name="A2" filename="lib/A.cs"><lines>
      <line number="2" hits="1"/>
    </lines></class>
    <class name="B" filename="lib/B.cs"><lines>
      <line number="1" hits="0"/>
    </lines></class>
  </classes></package></packages>
</coverage>`

// Cobertura yields line counts, never findings: coverage gaps do not drive
// tests (R7). A line two classes report counts once, covered if either
// reached it.
func TestCobertura(t *testing.T) {
	root := t.TempDir()
	out := write(t, root, "cov.xml", strings.ReplaceAll(cobertura, "SRC", root))
	res := Findings(Input{Command: "coverage", Format: "cobertura", Root: root, Outputs: []string{out}, Declared: true})
	if len(res.Findings) != 0 || res.Coverage == nil || *res.Coverage != (report.LineCounts{Valid: 4, Covered: 3}) {
		t.Fatalf("result = %+v %+v", res.Findings, res.Coverage)
	}
	res = Findings(Input{Command: "coverage", Format: "cobertura", Root: root, Outputs: []string{out}, Declared: true,
		Changed: map[string]map[int]bool{"lib/A.cs": {2: true}, "lib/B.cs": {1: true}}, Base: "main"})
	if res.Coverage == nil || *res.Coverage != (report.LineCounts{Valid: 2, Covered: 1, ChangedSince: "main"}) {
		t.Errorf("changed-only coverage = %+v", res.Coverage)
	}
	// The exit code still counts, and a file that is not Cobertura is a
	// finding rather than silently ignored.
	if got := Findings(Input{Command: "coverage", Format: "cobertura", Root: root, Outputs: []string{out}, Declared: true, ExitCode: 1}).Findings; len(got) != 1 {
		t.Errorf("failing exit: %+v", got)
	}
	// Class file names are relative to the report's first source, which
	// need not be the repository root; a report may have no sources, and a
	// file name may be absolute.
	sub := write(t, root, "sub.xml", strings.ReplaceAll(cobertura, "SRC", filepath.Join(root, "app")))
	res = Findings(Input{Command: "coverage", Format: "cobertura", Root: root, Outputs: []string{sub}, Declared: true,
		Changed: map[string]map[int]bool{"app/lib/B.cs": {1: true}}, Base: "main"})
	if res.Coverage == nil || *res.Coverage != (report.LineCounts{Valid: 1, ChangedSince: "main"}) {
		t.Errorf("source below the root: %+v", res.Coverage)
	}
	abs := filepath.ToSlash(filepath.Join(root, "lib", "C.cs"))
	none := write(t, root, "none.xml", `<coverage><packages><package><classes>
	  <class filename="`+abs+`"><lines><line number="4" hits="2"/></lines></class>
	</classes></package></packages></coverage>`)
	res = Findings(Input{Command: "coverage", Format: "cobertura", Root: root, Outputs: []string{none}, Declared: true,
		Changed: map[string]map[int]bool{"lib/C.cs": {4: true}}, Base: "main"})
	if res.Coverage == nil || *res.Coverage != (report.LineCounts{Valid: 1, Covered: 1, ChangedSince: "main"}) {
		t.Errorf("no sources, absolute file name: %+v", res.Coverage)
	}
	bad := write(t, root, "bad.xml", "<nope/>")
	if got := Findings(Input{Command: "coverage", Format: "cobertura", Outputs: []string{bad}, Declared: true}).Findings; len(got) != 1 || !strings.Contains(got[0].Message, "Cobertura") {
		t.Errorf("malformed: %+v", got)
	}
	if got := Findings(Input{Command: "coverage", Format: "cobertura", Declared: true, Display: "cov.xml"}).Findings; len(got) != 1 || !strings.Contains(got[0].Message, "not produced") {
		t.Errorf("missing: %+v", got)
	}
}

func jsonString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}
