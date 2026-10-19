package adapters

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hbaldwin98/reliab/internal/report"
)

// Check is what reliab findings check reports on an adapter's findings file:
// the findings verify would read, and what would make them unstable or
// misleading once they reach a report.
type Check struct {
	Findings    []Failure
	Mutants     *report.MutantCounts
	MutantFiles map[string]report.FileCounts
	Warnings    []string
}

var (
	// A test that names a line ("src/a.rs:42", "line 42") changes id when
	// code above it moves, and loses its classification.
	lineInTest = regexp.MustCompile(`(?i)\.[a-z0-9]+:\d+|\bline \d+\b`)
	// A drive letter: a path only this machine has.
	drivePath = regexp.MustCompile(`\b[A-Za-z]:[\\/]`)
)

// CheckFindings parses a findings file as verify does and lists its hazards
// (knowledge/decisions/external-adapters.md). A file verify would reject is
// an error. root is the repository root, against which paths are checked;
// with root empty they are not.
func CheckFindings(raw []byte, root string) (Check, error) {
	fs, err := ParseFindings(raw)
	if err != nil {
		return Check{}, err
	}
	c := Check{Findings: fs, Mutants: FindingsCounts(raw), MutantFiles: FindingsFileCounts(raw)}
	warn := func(format string, args ...any) { c.Warnings = append(c.Warnings, fmt.Sprintf(format, args...)) }

	for _, f := range fs {
		if lineInTest.MatchString(f.Test) {
			warn("test %q names a line: its id changes when code above it moves, and its classification is lost. Keep the line in location only", f.Test)
		}
		if machinePath(f.Test, root) {
			warn("test %q holds a path from this machine: another checkout gives the same failure another id. Use a path relative to the repository", f.Test)
		}
		if f.Location != "" && root != "" {
			file, _ := splitLocation(f.Location)
			if msg := repoFile(root, file); msg != "" {
				warn("location %q: %s", f.Location, msg)
			}
		}
	}

	if m := c.Mutants; m != nil && m.Survived+m.NotCovered > len(fs) {
		warn("mutants counts %d survived and %d not covered, but the file has %d findings: each one needs a finding", m.Survived, m.NotCovered, len(fs))
	}
	if len(c.MutantFiles) > 0 {
		var sum report.FileCounts
		files := make([]string, 0, len(c.MutantFiles))
		for f, n := range c.MutantFiles {
			files = append(files, f)
			sum.Killed += n.Killed
			sum.Survived += n.Survived
			sum.NotCovered += n.NotCovered
		}
		sort.Strings(files)
		if m := c.Mutants; m == nil {
			warn("mutantFiles without mutants: the summary reads the totals from mutants")
		} else if sum.Killed != m.Killed || sum.Survived != m.Survived || sum.NotCovered != m.NotCovered {
			warn("mutantFiles add up to %d killed, %d survived, %d not covered, but mutants says %d, %d, %d", sum.Killed, sum.Survived, sum.NotCovered, m.Killed, m.Survived, m.NotCovered)
		}
		if root != "" {
			for _, f := range files {
				if msg := repoFile(root, f); msg != "" {
					warn("mutantFiles %q: %s", f, msg)
				}
			}
		}
	}
	return c, nil
}

// machinePath reports whether s holds a path that exists only on this
// machine: the repository's own absolute path, the temp directory, or a
// drive letter.
func machinePath(s, root string) bool {
	s = strings.ToLower(filepath.ToSlash(s))
	for _, p := range []string{root, os.TempDir()} {
		if p != "" && strings.Contains(s, strings.TrimSuffix(strings.ToLower(filepath.ToSlash(p)), "/")+"/") {
			return true
		}
	}
	return drivePath.MatchString(s)
}

// repoFile says what is wrong with file as a path in the repository, or ""
// when it names a file there. reliab makes an absolute path inside the
// repository relative, so only one outside it, or a relative one that is
// not there (relative to the tool's working directory, not the root), is
// wrong.
func repoFile(root, file string) string {
	r := rel(root, file)
	if filepath.IsAbs(filepath.FromSlash(r)) || r == ".." || strings.HasPrefix(r, "../") {
		return file + " is outside the repository"
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(r))); err != nil {
		return r + " is not a file in the repository: paths are relative to the repository root, not the tool's working directory"
	}
	return ""
}
