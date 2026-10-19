package export

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

func sample() *report.Report {
	return &report.Report{
		SchemaVersion: report.SchemaVersion,
		Tool:          report.Tool{Name: "reliab", Version: "v0.1.0"},
		Run: report.Run{
			Tier: "prepr", Profile: "p", StartedAt: "2026-10-17T00:00:00Z", Complete: true,
			TreeHash: strings.Repeat("a", 40),
			Commands: []report.CommandResult{{ID: "build"}, {ID: "unit", ExitCode: 1, ElapsedSec: 2}},
			Skipped:  []report.Skip{{ID: "mutation_changed", Reason: report.SkipBudget}},
		},
		Findings: []report.Finding{
			{ID: "F-unit-1", Kind: report.KindNewCodeDefect, Command: "unit", Test: "pkg::t1", Location: "src/a.rs:42", Message: "boom", Status: report.StatusOpen},
			{ID: "F-unit-2", Kind: report.KindCharacterizationDiff, Command: "unit", Test: "pkg::t2", Status: report.StatusOpen},
			{ID: "F-unit-3", Kind: report.KindLegacyDefectOrQuirk, Command: "unit", Test: "pkg::t3", Status: report.StatusAcknowledged},
		},
	}
}

func TestSARIF(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sample(), "sarif"); err != nil {
		t.Fatal(err)
	}
	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Results []struct {
				RuleID    string `json:"ruleId"`
				Level     string `json:"level"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct{ URI string } `json:"artifactLocation"`
						Region           *struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	res := log.Runs[0].Results
	if log.Version != "2.1.0" || len(res) != 3 {
		t.Fatalf("version %s, %d results", log.Version, len(res))
	}
	if res[0].RuleID != "new-code-defect" || res[0].Level != "error" {
		t.Errorf("first result %+v", res[0])
	}
	loc := res[0].Locations[0].PhysicalLocation
	if loc.ArtifactLocation.URI != "src/a.rs" || loc.Region == nil || loc.Region.StartLine != 42 {
		t.Errorf("location %+v", loc)
	}
	if res[1].Level != "warning" || res[2].Level != "none" {
		t.Errorf("levels: human kind %q, acknowledged %q", res[1].Level, res[2].Level)
	}
}

func TestJUnit(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sample(), "junit"); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
		Skipped  int `xml:"skipped,attr"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err, buf.String())
	}
	// build passes (1), unit has two open findings (2 failures), one skip.
	if doc.Tests != 4 || doc.Failures != 2 || doc.Skipped != 1 {
		t.Fatalf("tests=%d failures=%d skipped=%d\n%s", doc.Tests, doc.Failures, doc.Skipped, buf.String())
	}
}

func TestUnknownFormat(t *testing.T) {
	if err := Write(&bytes.Buffer{}, sample(), "html"); err == nil {
		t.Fatal("unknown format accepted")
	}
}
