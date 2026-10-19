package adapters

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// JUnit XML as written by gotestsum, nextest, pytest, Jest, Maven, and
// others. Suites may nest; the root may be <testsuites> or a <testsuite>.
type junitSuite struct {
	Suites []junitSuite `xml:"testsuite"`
	Cases  []junitCase  `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	File      string        `xml:"file,attr"`
	Line      string        `xml:"line,attr"`
	Failures  []junitDetail `xml:"failure"`
	Errors    []junitDetail `xml:"error"`
}

type junitDetail struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

// ParseJUnit returns the failing and erroring test cases in a JUnit report.
func ParseJUnit(raw []byte) ([]Failure, error) {
	var root junitSuite
	if err := xml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	var out []Failure
	var walk func(s junitSuite)
	walk = func(s junitSuite) {
		for _, c := range s.Cases {
			details := append(append([]junitDetail{}, c.Failures...), c.Errors...)
			if len(details) == 0 {
				continue
			}
			name := c.Name
			if c.Classname != "" {
				name = c.Classname + "::" + c.Name
			}
			loc := c.File
			if loc != "" && c.Line != "" {
				loc += ":" + c.Line
			}
			var msg []string
			for _, d := range details {
				msg = append(msg, strings.TrimSpace(strings.Join(nonEmpty(d.Type, d.Message, d.Text), "\n")))
			}
			out = append(out, Failure{Test: name, Location: loc, Message: strings.Join(msg, "\n\n")})
		}
		for _, sub := range s.Suites {
			walk(sub)
		}
	}
	walk(root)
	return out, nil
}

// TRX, the Visual Studio test results format. Element names are matched
// without their namespace. A result's testName is often the bare method
// name, so its test definition supplies the class.
type trxRun struct {
	XMLName     xml.Name    `xml:"TestRun"`
	Definitions []trxTest   `xml:"TestDefinitions>UnitTest"`
	Results     []trxResult `xml:"Results>UnitTestResult"`
}

type trxTest struct {
	ID     string `xml:"id,attr"`
	Method struct {
		ClassName string `xml:"className,attr"`
	} `xml:"TestMethod"`
}

type trxResult struct {
	TestID   string `xml:"testId,attr"`
	TestName string `xml:"testName,attr"`
	Outcome  string `xml:"outcome,attr"`
	Message  string `xml:"Output>ErrorInfo>Message"`
	Stack    string `xml:"Output>ErrorInfo>StackTrace"`
}

// ParseTRX returns the failed results in a TRX report, each named by its
// class and test name.
func ParseTRX(raw []byte) ([]Failure, error) {
	var run trxRun
	if err := xml.Unmarshal(raw, &run); err != nil {
		return nil, fmt.Errorf("not a TRX TestRun: %w", err)
	}
	class := map[string]string{}
	for _, d := range run.Definitions {
		class[d.ID] = d.Method.ClassName
	}
	var out []Failure
	for _, r := range run.Results {
		switch r.Outcome {
		case "Failed", "Error", "Timeout", "Aborted":
			name := r.TestName
			// Some adapters already write the qualified name.
			if c := class[r.TestID]; c != "" && !strings.HasPrefix(name, c+".") {
				name = c + "." + name
			}
			out = append(out, Failure{
				Test:    name,
				Message: strings.Join(nonEmpty(r.Outcome, strings.TrimSpace(r.Message), strings.TrimSpace(r.Stack)), "\n"),
			})
		}
	}
	return out, nil
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
