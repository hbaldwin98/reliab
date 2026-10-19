// Package export renders a report in standard formats so any CI test tab or
// code-scanning view can show it: SARIF 2.1.0 for findings and JUnit XML for
// commands. See knowledge/decisions/schemas-and-versioning.md.
package export

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/hbaldwin98/reliab/internal/report"
)

// Formats lists the export formats.
var Formats = []string{"sarif", "junit"}

// Write renders r in format to w.
func Write(w io.Writer, r *report.Report, format string) error {
	switch format {
	case "sarif":
		return sarif(w, r)
	case "junit":
		return junit(w, r)
	}
	return fmt.Errorf("format %q: want one of %s", format, strings.Join(Formats, ", "))
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string       `json:"id"`
	ShortDescription sarifMessage `json:"shortDescription"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID              string          `json:"ruleId"`
	Level               string          `json:"level"`
	Kind                string          `json:"kind,omitempty"`
	Message             sarifMessage    `json:"message"`
	Locations           []sarifLocation `json:"locations,omitempty"`
	Properties          map[string]any  `json:"properties,omitempty"`
	PartialFingerprints map[string]any  `json:"partialFingerprints,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           *sarifRegion  `json:"region,omitempty"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

var ruleText = map[string]string{
	report.KindUnclassified:         "Failure not yet classified",
	report.KindNewCodeDefect:        "Defect in new code",
	report.KindHarnessDefect:        "Defect in the test harness",
	report.KindWrongProperty:        "Property does not hold as written",
	report.KindLegacyDefectOrQuirk:  "Legacy defect or quirk awaiting a human decision",
	report.KindCharacterizationDiff: "Characterization snapshot changed",
	report.KindEquivalentMutant:     "Surviving mutant judged equivalent, awaiting a human",
	report.KindRequirementSilent:    "Surviving mutant on behavior no requirement states, awaiting a human",
	report.KindEnvironment:          "Failure caused by the machine, not the code, awaiting a human",
}

func sarif(w io.Writer, r *report.Report) error {
	kinds := append(append([]string{}, report.AgentKinds...), report.HumanKinds...)
	rules := make([]sarifRule, 0, len(kinds))
	for _, k := range kinds {
		rules = append(rules, sarifRule{ID: k, ShortDescription: sarifMessage{ruleText[k]}})
	}
	results := make([]sarifResult, 0, len(r.Findings))
	for _, f := range r.Findings {
		res := sarifResult{
			RuleID:              f.Kind,
			Level:               "error",
			Message:             sarifMessage{Text: findingText(f)},
			Properties:          map[string]any{"reliabId": f.ID, "command": f.Command, "status": f.Status},
			PartialFingerprints: map[string]any{"reliabId/v1": f.ID},
		}
		if f.Status != report.StatusOpen {
			res.Kind, res.Level = "informational", "none"
		} else if report.NeedsHuman(f) {
			res.Level = "warning"
		}
		if loc := location(f.Location); loc != nil {
			res.Locations = []sarifLocation{*loc}
		}
		results = append(results, res)
	}
	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool:    sarifTool{Driver: sarifDriver{Name: "reliab", Version: r.Tool.Version, Rules: rules}},
			Results: results,
		}},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

// location parses "path:line" or "path" into a SARIF location.
func location(s string) *sarifLocation {
	if s == "" {
		return nil
	}
	path, region := s, (*sarifRegion)(nil)
	if i := strings.LastIndex(s, ":"); i > 1 { // i > 1 keeps C:\ drive letters intact
		if n, err := strconv.Atoi(s[i+1:]); err == nil && n > 0 {
			path, region = s[:i], &sarifRegion{StartLine: n}
		}
	}
	return &sarifLocation{sarifPhysical{ArtifactLocation: sarifArtifact{URI: strings.ReplaceAll(path, "\\", "/")}, Region: region}}
}

func findingText(f report.Finding) string {
	var b strings.Builder
	if f.Test != "" {
		fmt.Fprintf(&b, "%s: ", f.Test)
	} else {
		fmt.Fprintf(&b, "command %s: ", f.Command)
	}
	switch {
	case f.Observed != "":
		fmt.Fprintf(&b, "observed %s", f.Observed)
		if f.Expected != "" {
			fmt.Fprintf(&b, ", expected %s", f.Expected)
		}
	case f.Message != "":
		b.WriteString(f.Message)
	default:
		b.WriteString(ruleText[f.Kind])
	}
	return b.String()
}

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Time     string      `xml:"time,attr,omitempty"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr,omitempty"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

// junit writes one suite per command. A command passes as one test case
// when it has no open findings; each open finding is a failing case;
// skipped commands are skipped cases.
func junit(w io.Writer, r *report.Report) error {
	byCmd := map[string][]report.Finding{}
	for _, f := range r.Findings {
		if f.Status == report.StatusOpen {
			byCmd[f.Command] = append(byCmd[f.Command], f)
		}
	}
	doc := junitSuites{Name: "reliab " + r.Run.Tier}
	for _, c := range r.Run.Commands {
		s := junitSuite{Name: c.ID, Time: fmt.Sprintf("%.1f", c.ElapsedSec)}
		if fs := byCmd[c.ID]; len(fs) > 0 {
			for _, f := range fs {
				name := f.Test
				if name == "" {
					name = c.ID
				}
				s.Cases = append(s.Cases, junitCase{
					Name: name, Classname: c.ID,
					Failure: &junitFailure{Message: f.ID + " " + f.Kind, Type: f.Kind, Text: f.Message},
				})
				s.Failures++
			}
		} else {
			s.Cases = append(s.Cases, junitCase{Name: c.ID, Classname: c.ID, Time: s.Time})
		}
		s.Tests = len(s.Cases)
		doc.Suites = append(doc.Suites, s)
	}
	for _, sk := range r.Run.Skipped {
		msg := sk.Reason
		if sk.Detail != "" {
			msg += ": " + sk.Detail
		}
		doc.Suites = append(doc.Suites, junitSuite{
			Name: sk.ID, Tests: 1, Skipped: 1,
			Cases: []junitCase{{Name: sk.ID, Classname: sk.ID, Skipped: &junitSkipped{Message: msg}}},
		})
	}
	for _, s := range doc.Suites {
		doc.Tests += s.Tests
		doc.Failures += s.Failures
		doc.Skipped += s.Skipped
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
