package schemas

import (
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func loadJSON(t *testing.T, path string) any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestExampleReportValidates(t *testing.T) {
	if err := Validate(Report, loadJSON(t, "testdata/inner.report.json")); err != nil {
		t.Fatal(err)
	}
}

func TestReportRejections(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(m map[string]any)
		want   string
	}{
		{"unknown top-level key", func(m map[string]any) { m["extra"] = true }, "additional properties"},
		{"unknown finding kind", func(m map[string]any) {
			m["findings"].([]any)[0].(map[string]any)["kind"] = "bug"
		}, "kind"},
		{"unknown command id", func(m map[string]any) {
			m["run"].(map[string]any)["commands"].([]any)[0].(map[string]any)["id"] = "lint"
		}, "id"},
		{"bad tree hash", func(m map[string]any) { m["run"].(map[string]any)["treeHash"] = "HEAD" }, "treeHash"},
		{"bad timestamp", func(m map[string]any) { m["run"].(map[string]any)["startedAt"] = "yesterday" }, "date-time"},
		{"future major", func(m map[string]any) { m["schemaVersion"] = "1.0.0" }, "supports major version 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := loadJSON(t, "testdata/inner.report.json").(map[string]any)
			tt.mutate(doc)
			err := Validate(Report, doc)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

// knowledge/decisions/snapshot-diff-labels.md: findings and reports carry an
// optional diff label, one of two values, and older files without it still
// validate.
func TestDiffLabel(t *testing.T) {
	findings := func(diff string) any {
		f := map[string]any{"test": "snapshots/a.snap"}
		if diff != "" {
			f["diff"] = diff
		}
		return map[string]any{"schemaVersion": "0.1.0", "findings": []any{f}}
	}
	for _, ok := range []string{"", "additive-only", "values-changed"} {
		if err := Validate(Findings, findings(ok)); err != nil {
			t.Errorf("findings diff %q: %v", ok, err)
		}
		doc := loadJSON(t, "testdata/inner.report.json").(map[string]any)
		if ok != "" {
			doc["findings"].([]any)[0].(map[string]any)["diff"] = ok
		}
		if err := Validate(Report, doc); err != nil {
			t.Errorf("report diff %q: %v", ok, err)
		}
	}
	if err := Validate(Findings, findings("changed")); err == nil {
		t.Error("findings accepted an unknown diff label")
	}
	doc := loadJSON(t, "testdata/inner.report.json").(map[string]any)
	doc["findings"].([]any)[0].(map[string]any)["diff"] = "changed"
	if err := Validate(Report, doc); err == nil {
		t.Error("report accepted an unknown diff label")
	}
}

// knowledge/decisions/on-demand-characterization.md: characterize is a
// command id like the others.
func TestCharacterizeCommandID(t *testing.T) {
	doc := loadJSON(t, "testdata/inner.report.json").(map[string]any)
	doc["run"].(map[string]any)["commands"].([]any)[0].(map[string]any)["id"] = "characterize"
	if err := Validate(Report, doc); err != nil {
		t.Fatal(err)
	}
}

// knowledge/decisions/pr-evidence-summary.md and reviewer-verdict.md: run
// head, mutant counts, and the verdict's binding are optional additions, so
// the 0.1.0 example (which has none of them) still validates.
func TestSummaryAndVerdictFields(t *testing.T) {
	add := func(edit func(m map[string]any)) error {
		doc := loadJSON(t, "testdata/inner.report.json").(map[string]any)
		edit(doc)
		return Validate(Report, doc)
	}
	run := func(m map[string]any) map[string]any { return m["run"].(map[string]any) }
	cmd := func(m map[string]any) map[string]any { return run(m)["commands"].([]any)[0].(map[string]any) }
	hash := strings.Repeat("a", 40)
	good := map[string]func(map[string]any){
		"head": func(m map[string]any) { run(m)["head"] = hash },
		"mutants": func(m map[string]any) {
			cmd(m)["mutants"] = map[string]any{"killed": 3, "survived": 1, "notCovered": 0}
		},
		"verdict": func(m map[string]any) {
			m["verdict"] = map[string]any{"outcome": "pass", "by": "reliab:reviewer", "at": "2026-10-18T10:00:00Z", "treeHash": hash, "head": hash, "rerunStartedAt": "2026-10-18T09:50:00Z"}
		},
	}
	for name, edit := range good {
		if err := add(edit); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := map[string]func(map[string]any){
		"head not a hash":   func(m map[string]any) { run(m)["head"] = "HEAD" },
		"negative count":    func(m map[string]any) { cmd(m)["mutants"] = map[string]any{"killed": -1, "survived": 0} },
		"counts incomplete": func(m map[string]any) { cmd(m)["mutants"] = map[string]any{"killed": 1} },
		"verdict tree": func(m map[string]any) {
			m["verdict"] = map[string]any{"outcome": "pass", "by": "r", "at": "2026-10-18T10:00:00Z", "treeHash": "x"}
		},
	}
	for name, edit := range bad {
		if err := add(edit); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	counted := map[string]any{"schemaVersion": "0.3.0", "mutants": map[string]any{"killed": 2, "survived": 0}, "findings": []any{}}
	if err := Validate(Findings, counted); err != nil {
		t.Errorf("findings with counts: %v", err)
	}
}
