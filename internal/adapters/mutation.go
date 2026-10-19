package adapters

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/pkg/findings"
)

// Each surviving or uncovered mutant is a finding; timeouts count as caught.

// The mutation-testing-elements report (format "mutation-report"), written
// by Stryker and others.
type mutationReport struct {
	SchemaVersion string `json:"schemaVersion"`
	ProjectRoot   string `json:"projectRoot"`
	Files         map[string]struct {
		Source  string `json:"source"`
		Mutants []struct {
			MutatorName string `json:"mutatorName"`
			Replacement string `json:"replacement"`
			Description string `json:"description"`
			Status      string `json:"status"`
			Location    struct {
				Start position `json:"start"`
				End   position `json:"end"`
			} `json:"location"`
		} `json:"mutants"`
	} `json:"files"`
}

// position is 1-based, as the schema defines it.
type position struct{ Line, Column int }

// ParseMutationReport returns the survived and uncovered mutants in a
// mutation-testing-elements report, with paths as the report names them.
func ParseMutationReport(raw []byte) ([]Failure, error) {
	p, err := parseMutationReport(raw, Input{})
	return p.fails, err
}

// parseMutationReport reads a report. File keys are made relative to the
// repository root (tools often write them absolute), so a mutant keeps its
// id on any checkout. With in.Changed set, only mutants on changed lines
// become findings or are counted.
func parseMutationReport(raw []byte, in Input) (parsed, error) {
	var doc mutationReport
	if err := json.Unmarshal(raw, &doc); err != nil {
		return parsed{}, fmt.Errorf("not a mutation-testing-elements report: %w", err)
	}
	if doc.SchemaVersion == "" || doc.Files == nil {
		return parsed{}, fmt.Errorf("not a mutation-testing-elements report: no schemaVersion or files")
	}
	var c report.MutantCounts
	files := map[string]report.FileCounts{}
	var ms []findings.Mutant
	for key, f := range doc.Files {
		file := key
		if doc.ProjectRoot != "" && !filepath.IsAbs(filepath.FromSlash(file)) {
			file = filepath.Join(doc.ProjectRoot, filepath.FromSlash(file))
		}
		file = rel(in.Root, file)
		lines := strings.Split(strings.ReplaceAll(f.Source, "\r\n", "\n"), "\n")
		for _, m := range f.Mutants {
			if in.Changed != nil && !in.Changed[file][m.Location.Start.Line] {
				continue
			}
			var detail string
			fc := files[file]
			switch m.Status {
			case "Killed", "Timeout":
				c.Killed++
				fc.Killed++
				files[file] = fc
				continue
			case "Survived":
				c.Survived++
				fc.Survived++
			case "NoCoverage":
				c.NotCovered++
				fc.NotCovered++
				detail = "no test covers this line"
			default:
				continue
			}
			files[file] = fc
			desc := m.MutatorName
			if m.Replacement != "" {
				desc += ": replace with " + m.Replacement
			}
			ms = append(ms, findings.Mutant{
				File:        file,
				Description: desc,
				Detail:      strings.Join(nonEmpty(m.Description, detail, mutatedLines(lines, m.Location.Start, m.Location.End, m.Replacement)), "\n"),
				Line:        m.Location.Start.Line,
				Col:         m.Location.Start.Column,
			})
		}
	}
	return parsed{fails: findings.Mutants(ms), counts: &c, files: files}, nil
}

// mutatedLines renders the mutated lines as a diff: the source lines the
// mutant spans, then the same lines with the span replaced. It is empty
// when the report carries no source or the location falls outside it.
func mutatedLines(lines []string, start, end position, replacement string) string {
	if len(lines) == 0 || start.Line < 1 || end.Line < start.Line || end.Line > len(lines) {
		return ""
	}
	first, last := lines[start.Line-1], lines[end.Line-1]
	if start.Column < 1 || start.Column-1 > len(first) || end.Column < 1 || end.Column-1 > len(last) {
		return ""
	}
	if start.Line == end.Line && end.Column < start.Column {
		return ""
	}
	mutated := first[:start.Column-1] + replacement + last[end.Column-1:]
	var b strings.Builder
	for _, l := range lines[start.Line-1 : end.Line] {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	for _, l := range strings.Split(mutated, "\n") {
		fmt.Fprintf(&b, "+ %s\n", l)
	}
	return strings.TrimRight(b.String(), "\n")
}
