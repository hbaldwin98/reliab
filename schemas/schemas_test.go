package schemas

import "testing"

func findingsDoc(f map[string]any) map[string]any {
	return map[string]any{"schemaVersion": "0.1.0", "findings": []any{f}}
}

// knowledge/decisions/snapshot-diff-labels.md: a finding carries an
// optional diff label, one of two values, and files without it validate.
func TestDiffLabel(t *testing.T) {
	for _, ok := range []string{"", "additive-only", "values-changed"} {
		f := map[string]any{"test": "snapshots/a.snap"}
		if ok != "" {
			f["diff"] = ok
		}
		if err := Validate(Findings, findingsDoc(f)); err != nil {
			t.Errorf("diff %q: %v", ok, err)
		}
	}
	if err := Validate(Findings, findingsDoc(map[string]any{"test": "a", "diff": "changed"})); err == nil {
		t.Error("accepted an unknown diff label")
	}
}

// docs/adapters.md: a mutation adapter reports counts, inlined from the
// definitions the findings schema owns.
func TestMutantCounts(t *testing.T) {
	doc := findingsDoc(map[string]any{"test": "a"})
	doc["mutants"] = map[string]any{"killed": 3.0, "survived": 1.0, "notCovered": 0.0}
	doc["mutantFiles"] = map[string]any{"a.go": map[string]any{"killed": 3.0, "survived": 1.0}}
	if err := Validate(Findings, doc); err != nil {
		t.Fatal(err)
	}
	doc["mutants"] = map[string]any{"killed": -1.0, "survived": 0.0}
	if err := Validate(Findings, doc); err == nil {
		t.Error("accepted a negative count")
	}
}

// A document whose schemaVersion has another major is rejected.
func TestMajorVersion(t *testing.T) {
	doc := findingsDoc(map[string]any{"test": "a"})
	doc["schemaVersion"] = "1.0.0"
	if err := Validate(Findings, doc); err == nil {
		t.Error("accepted findings major 1")
	}
	doc["schemaVersion"] = "x"
	if err := Validate(Findings, doc); err == nil {
		t.Error("accepted a non-semver schemaVersion")
	}
}
