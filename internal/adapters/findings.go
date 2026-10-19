package adapters

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/schemas"
)

// ParseFindings reads reliab's own findings format (pkg/findings), which
// adapters write for tools reliab does not read itself.
func ParseFindings(raw []byte) ([]Failure, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if err := schemas.Validate(schemas.Findings, doc); err != nil {
		return nil, err
	}
	var file struct {
		Findings []Failure `json:"findings"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range file.Findings {
		if seen[f.Test] {
			return nil, fmt.Errorf("test %q appears twice; each finding needs its own test", f.Test)
		}
		seen[f.Test] = true
	}
	return file.Findings, nil
}

// FindingsCounts returns the mutant counts a findings file carries, if any.
// The file has already been validated.
func FindingsCounts(raw []byte) *report.MutantCounts {
	var file struct {
		Mutants *report.MutantCounts `json:"mutants"`
	}
	if json.Unmarshal(raw, &file) != nil {
		return nil
	}
	return file.Mutants
}

// FindingsFileCounts returns the per-file mutant counts a findings file
// carries, if any. The file has already been validated.
func FindingsFileCounts(raw []byte) map[string]report.FileCounts {
	var file struct {
		MutantFiles map[string]report.FileCounts `json:"mutantFiles"`
	}
	if json.Unmarshal(raw, &file) != nil {
		return nil
	}
	return file.MutantFiles
}
