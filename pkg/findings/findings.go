// Package findings is reliab's own findings format, which adapters write for
// tools reliab does not read itself (schemas/findings.schema.json,
// knowledge/decisions/external-adapters.md).
package findings

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
)

// SchemaVersion is the version adapters in this repo write.
const SchemaVersion = "0.4.0"

// Finding is one failure as a format reports it. Test is its identity: the
// same failure must get the same Test across runs.
type Finding struct {
	Test     string `json:"test"`
	Location string `json:"location,omitempty"`
	Message  string `json:"message,omitempty"`
	Replay   string `json:"replay,omitempty"`
	Observed string `json:"observed,omitempty"`
	Expected string `json:"expected,omitempty"`
	// Diff labels a text snapshot that no longer matches (pkg/snapdiff).
	Diff string `json:"diff,omitempty"`
}

// MutantCounts is how many mutants a mutation tool tested: killed
// (timeouts included), survived, and not covered by any test.
type MutantCounts struct {
	Killed     int `json:"killed"`
	Survived   int `json:"survived"`
	NotCovered int `json:"notCovered,omitempty"`
}

// Write writes fs to path as a findings file.
func Write(path string, fs []Finding) error { return WriteMutants(path, fs, nil) }

// WriteMutants writes fs to path as a findings file with a mutation tool's
// counts, when counts is not nil.
func WriteMutants(path string, fs []Finding, counts *MutantCounts) error {
	return WriteMutantFiles(path, fs, counts, nil)
}

// WriteMutantFiles is WriteMutants with the counts per repo-relative file
// too, so reliab can score a baseline scope
// (knowledge/decisions/mutation-threshold.md).
func WriteMutantFiles(path string, fs []Finding, counts *MutantCounts, files map[string]MutantCounts) error {
	if fs == nil {
		fs = []Finding{}
	}
	if len(files) == 0 {
		files = nil
	}
	raw, err := json.MarshalIndent(struct {
		SchemaVersion string                  `json:"schemaVersion"`
		Mutants       *MutantCounts           `json:"mutants,omitempty"`
		MutantFiles   map[string]MutantCounts `json:"mutantFiles,omitempty"`
		Findings      []Finding               `json:"findings"`
	}{SchemaVersion, counts, files, fs}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// Mutant is one surviving mutant (rule R7: surviving mutants on changed
// lines drive new tests).
type Mutant struct {
	File, Description, Detail string
	Line, Col                 int
}

// Mutants converts surviving mutants to findings. A mutant has no stable
// name, so its Test is file and description without the line, which
// survives edits elsewhere in the file; mutants that share one are numbered
// in source order.
func Mutants(ms []Mutant) []Finding {
	ms = slices.Clone(ms)
	sort.SliceStable(ms, func(i, j int) bool {
		a, b := ms[i], ms[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	seen := map[string]int{}
	out := make([]Finding, 0, len(ms))
	for _, m := range ms {
		key := m.File + ": " + m.Description
		seen[key]++
		if n := seen[key]; n > 1 {
			key += fmt.Sprintf(" (#%d)", n)
		}
		msg := "surviving mutant: " + m.Description
		if d := strings.TrimSpace(m.Detail); d != "" {
			msg += "\n" + d
		}
		out = append(out, Finding{
			Test:     key,
			Location: fmt.Sprintf("%s:%d:%d", m.File, m.Line, m.Col),
			Message:  msg,
		})
	}
	return out
}
