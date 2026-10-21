package testresults

import "testing"

// knowledge/decisions/external-adapters.md: the core reads JUnit and TRX
// itself, and reliab exposes the rules adapters share as public Go
// packages; an adapter reading a tool's test report gets the same failures
// verify would. A failing or erroring case is a finding named by class and
// test, and a passing one is not.
func TestReaders(t *testing.T) {
	j, err := ParseJUnit([]byte(`<testsuites><testsuite><testcase classname="pkg" name="TestA" file="a_test.go" line="7"><failure message="want 2">got 1</failure></testcase><testcase name="TestOK"/></testsuite></testsuites>`))
	if err != nil || len(j) != 1 || j[0].Test != "pkg::TestA" || j[0].Location != "a_test.go:7" || j[0].Message != "want 2\ngot 1" {
		t.Errorf("JUnit = %+v, %v", j, err)
	}
	x, err := ParseTRX([]byte(`<TestRun xmlns="http://microsoft.com/schemas/VisualStudio/TeamTest/2010"><TestDefinitions><UnitTest id="1"><TestMethod className="Ns.C"/></UnitTest></TestDefinitions>` +
		`<Results><UnitTestResult testId="1" testName="M" outcome="Failed"><Output><ErrorInfo><Message>boom</Message></ErrorInfo></Output></UnitTestResult><UnitTestResult testId="1" testName="P" outcome="Passed"/></Results></TestRun>`))
	if err != nil || len(x) != 1 || x[0].Test != "Ns.C.M" || x[0].Message != "Failed\nboom" {
		t.Errorf("TRX = %+v, %v", x, err)
	}
	if _, err := ParseTRX([]byte(`<testsuite/>`)); err == nil {
		t.Error("a JUnit file read as TRX")
	}
}
