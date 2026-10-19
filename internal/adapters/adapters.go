// Package adapters turn tool output into report findings. Adapters are keyed
// by format, never by tool, so swapping a tool that writes the same format
// changes nothing here (paper P7).
package adapters

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/pkg/findings"
)

// Failure is one failing test as a format reports it.
type Failure = findings.Finding

// format describes how one output format is read.
type format struct {
	parse func(raw []byte, in Input) (parsed, error)
	// decisive: a parsed file is the whole result and the exit code is not
	// consulted. Mutation tools exit nonzero below a score threshold, and an
	// external adapter decides for itself what the tool's exit code meant.
	decisive bool
}

// parsed is what one result file holds: failures, and for a mutation
// format the mutants it tested.
type parsed struct {
	fails    []Failure
	counts   *report.MutantCounts
	files    map[string]report.FileCounts
	coverage *report.LineCounts
}

func plain(parse func([]byte) ([]Failure, error)) func([]byte, Input) (parsed, error) {
	return func(raw []byte, _ Input) (parsed, error) {
		fs, err := parse(raw)
		return parsed{fails: fs}, err
	}
}

// formats maps a format name to its reader. reliab reads only interchange
// formats that many tools write; anything tool-specific is converted to the
// findings format by an external adapter
// (knowledge/decisions/external-adapters.md). cobertura yields counts, not
// findings. sarif, which the profile schema allows but reliab cannot parse
// yet, is judged by exit code, but a declared output must still be written.
var formats = map[string]format{
	"junit":           {parse: plain(ParseJUnit)},
	"trx":             {parse: plain(ParseTRX)},
	"cobertura":       {parse: parseCobertura},
	"mutation-report": {parse: parseMutationReport, decisive: true},
	"findings": {parse: func(raw []byte, _ Input) (parsed, error) {
		fs, err := ParseFindings(raw)
		if err != nil {
			return parsed{}, err
		}
		return parsed{fails: fs, counts: FindingsCounts(raw), files: FindingsFileCounts(raw)}, nil
	}, decisive: true},
}

// Supported reports whether format has a parser.
func Supported(name string) bool { _, ok := formats[name]; return ok }

// MaxMessage bounds the raw failure text kept in a finding.
const MaxMessage = 2000

// Input is one command's result as verify hands it over.
type Input struct {
	Command string
	Format  string
	// Root is the repository root. Paths in findings are made relative to
	// it, so a finding keeps its id on any checkout.
	Root string
	// Outputs are the result files the command wrote, absolute: every match
	// of the profile's output path or glob. Display is that path or glob as
	// the profile names it; Declared is false when the profile names none.
	Outputs  []string
	Display  string
	Declared bool
	ExitCode int
	// LogTail is the end of the command's captured output, used when there
	// is nothing better to report.
	LogTail string
	// Changed, when set, holds the lines changed since the base (repo-relative
	// path -> line -> true). Mutation-report mutants elsewhere are neither
	// reported nor counted (R7: surviving mutants on changed lines).
	Changed map[string]map[int]bool
	// Base names the ref Changed was computed against, for the counts.
	Base string
}

// Result is one command's findings, plus counts when its format has them:
// mutants for a mutation format, lines for a coverage format. Under a
// changed-lines filter the counts say which base they were measured from.
type Result struct {
	Findings []report.Finding
	Mutants  *report.MutantCounts
	// MutantFiles are the mutant counts per file, when the format says.
	MutantFiles map[string]report.FileCounts
	Coverage    *report.LineCounts
}

// Findings converts one command's result into findings. Every finding starts
// unclassified and open (knowledge/decisions/finding-classification.md).
func Findings(in Input) Result {
	if in.Declared && len(in.Outputs) == 0 {
		if in.ExitCode == 0 {
			return Result{Findings: []report.Finding{generic(in.Command, fmt.Sprintf("exit 0 but output %s was not produced", in.Display))}}
		}
		return Result{Findings: []report.Finding{generic(in.Command, fmt.Sprintf("exit %d and no output %s\n%s", in.ExitCode, in.Display, in.LogTail))}}
	}
	var res Result
	var fails []Failure
	f, parses := formats[in.Format]
	if parses {
		for _, path := range in.Outputs {
			raw, err := os.ReadFile(path)
			if err != nil {
				return Result{Findings: []report.Finding{generic(in.Command, fmt.Sprintf("cannot read output %s: %v", rel(in.Root, path), err))}}
			}
			p, err := f.parse(raw, in)
			if err != nil {
				return Result{Findings: []report.Finding{generic(in.Command, fmt.Sprintf("cannot parse %s output %s: %v", in.Format, rel(in.Root, path), err))}}
			}
			fails = append(fails, p.fails...)
			res.Mutants = addCounts(res.Mutants, p.counts)
			res.MutantFiles = addFiles(res.MutantFiles, p.files)
			res.Coverage = addLines(res.Coverage, p.coverage)
		}
		if in.Changed != nil {
			if res.Mutants != nil {
				res.Mutants.ChangedSince = in.Base
			}
			if res.Coverage != nil {
				res.Coverage.ChangedSince = in.Base
			}
		}
	}
	if len(fails) > 0 {
		res.Findings = convert(in, fails)
		return res
	}
	if parses && f.decisive {
		return res
	}
	// The tool failed without reporting a failing test: a build or harness
	// error, judged by the exit code.
	if in.ExitCode != 0 {
		res.Findings = []report.Finding{generic(in.Command, fmt.Sprintf("exit %d\n%s", in.ExitCode, in.LogTail))}
	}
	return res
}

func addLines(a, b *report.LineCounts) *report.LineCounts {
	if b == nil {
		return a
	}
	if a == nil {
		c := *b
		return &c
	}
	a.Valid += b.Valid
	a.Covered += b.Covered
	return a
}

func addFiles(a, b map[string]report.FileCounts) map[string]report.FileCounts {
	if len(b) == 0 {
		return a
	}
	if a == nil {
		a = map[string]report.FileCounts{}
	}
	for f, c := range b {
		t := a[f]
		t.Killed += c.Killed
		t.Survived += c.Survived
		t.NotCovered += c.NotCovered
		a[f] = t
	}
	return a
}

func addCounts(a, b *report.MutantCounts) *report.MutantCounts {
	if b == nil {
		return a
	}
	if a == nil {
		c := *b
		return &c
	}
	a.Killed += b.Killed
	a.Survived += b.Survived
	a.NotCovered += b.NotCovered
	return a
}

// convert turns failures into findings. A test name a command reports more
// than once (the same method in two classes, or in two result files) is
// numbered in report order, so every finding has its own id.
func convert(in Input, fails []Failure) []report.Finding {
	out := make([]report.Finding, 0, len(fails))
	seen := map[string]int{}
	for _, fl := range fails {
		test := fl.Test
		seen[test]++
		if n := seen[test]; n > 1 {
			test += fmt.Sprintf(" (#%d)", n)
		}
		out = append(out, report.Finding{
			ID:       ID(in.Command, test),
			Kind:     report.KindUnclassified,
			Command:  in.Command,
			Test:     test,
			Location: relLocation(in.Root, fl.Location),
			Message:  truncate(fl.Message),
			Replay:   fl.Replay,
			Observed: fl.Observed,
			Expected: fl.Expected,
			Diff:     fl.Diff,
			Status:   report.StatusOpen,
		})
	}
	return out
}

func generic(commandID, msg string) report.Finding {
	return report.Finding{
		ID:      ID(commandID, ""),
		Kind:    report.KindUnclassified,
		Command: commandID,
		Message: truncate(msg),
		Status:  report.StatusOpen,
	}
}

// ID is a finding id that is stable across runs for the same command and
// test, so classifications can carry forward.
func ID(commandID, test string) string {
	if test == "" {
		return "F-" + strings.ReplaceAll(commandID, "_", "-")
	}
	sum := sha1.Sum([]byte(commandID + "\x00" + test))
	return "F-" + strings.ReplaceAll(commandID, "_", "-") + "-" + hex.EncodeToString(sum[:4])
}

// rel returns path relative to root, slash-separated, when it is absolute
// and inside root; otherwise path, slash-separated.
func rel(root, path string) string {
	p := filepath.FromSlash(path)
	if root == "" || !filepath.IsAbs(p) {
		return filepath.ToSlash(path)
	}
	r, err := filepath.Rel(root, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(r)
}

// relLocation makes the file part of a file:line[:col] location relative to
// root. A Windows drive letter's colon belongs to the file.
func relLocation(root, loc string) string {
	if loc == "" || root == "" {
		return loc
	}
	start := 0
	if len(loc) > 2 && loc[1] == ':' && (loc[2] == '/' || loc[2] == '\\') {
		start = 2
	}
	file, rest := loc, ""
	if i := strings.Index(loc[start:], ":"); i >= 0 {
		file, rest = loc[:start+i], loc[start+i:]
	}
	return rel(root, file) + rest
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= MaxMessage {
		return s
	}
	return s[:MaxMessage] + "…(truncated)"
}
