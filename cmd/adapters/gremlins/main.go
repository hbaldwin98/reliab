// Command reliab-adapter-gremlins converts gremlins JSON reports into
// reliab's findings format (knowledge/decisions/external-adapters.md).
//
//	reliab-adapter-gremlins [-base REF] findings.json REPORT...
//
// Each REPORT is a gremlins -o file (none means no package changed), or DIR=FILE when gremlins ran on package
// DIR, whose file names are then relative to DIR; a file in a directory
// under DIR is another package's and is left to that package's report. Lived and not-covered
// mutants become findings; timeouts count as caught. With -base, only
// mutants on lines changed since REF are kept: the lines reliab
// changed-lines prints (since the merge base with REF, uncommitted changes
// included), so every adapter and reliab itself agree on what changed. The findings file also counts the mutants kept:
// killed (timeouts included), lived, and not covered.
//
// Run gremlins once per package rather than with --diff from the module
// root: on Windows, gremlins 0.6.0 keys coverage by backslash paths and
// looks mutants up by slash paths, so from the root every mutant is reported
// not covered (go-gremlins/gremlins#222). Inside one package the paths have
// no separator and match.
//
// gremlins mutates every Go file in a package, also one this platform does
// not build (lock_unix.go on Windows), and calls its mutants not covered,
// since no test here can compile them. Their mutants are dropped and not
// counted: the platform that builds the file measures it.
//
// gremlins exits 0 with survivors, so its exit code is not consulted. With a
// report missing or unreadable, this exits 1 and writes nothing, and reliab
// reports the command as failed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/build"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/hbaldwin98/reliab/pkg/findings"
)

type report struct {
	GoModule string `json:"go_module"`
	Files    []struct {
		FileName  string `json:"file_name"`
		Mutations []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Line   int    `json:"line"`
			Column int    `json:"column"`
		} `json:"mutations"`
	} `json:"files"`
}

func main() { os.Exit(cli(os.Args[1:], os.Stderr)) }

// cli runs the adapter and returns its exit code: 0 written, 1 failed, 2
// usage.
func cli(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("reliab-adapter-gremlins", flag.ContinueOnError)
	fs.SetOutput(stderr)
	base := fs.String("base", "", "keep only mutants on lines changed since this git ref")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: reliab-adapter-gremlins [-base REF] <findings.json> <report.json | DIR=report.json>...")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// No reports is valid: the change touched no package.
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	if err := run(*base, fs.Arg(0), fs.Args()[1:]); err != nil {
		fmt.Fprintln(stderr, "reliab-adapter-gremlins:", err)
		return 1
	}
	return 0
}

func run(base, out string, reports []string) error {
	var keep func(file string, line int) bool
	if base != "" {
		changed, err := changedSince(base)
		if err != nil {
			return err
		}
		keep = func(file string, line int) bool { return changed[file][line] }
	}
	var ms []findings.Mutant
	var counts findings.MutantCounts
	files := map[string]findings.MutantCounts{}
	for _, arg := range reports {
		dir, file := "", arg
		if d, f, ok := strings.Cut(arg, "="); ok {
			dir, file = filepath.ToSlash(d), f
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		got, c, err := convertFiles(raw, dir, keep, files)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		ms = append(ms, got...)
		counts.Killed += c.Killed
		counts.Survived += c.Survived
		counts.NotCovered += c.NotCovered
	}
	return findings.WriteMutantFiles(out, findings.Mutants(ms), &counts, files)
}

// convert returns the lived and not-covered mutants in one report, and the
// counts of every mutant it tested. dir prefixes its file names; keep, when
// not nil, filters by changed lines. Mutants gremlins did not test (NOT
// VIABLE, SKIPPED) are not counted.
func convert(raw []byte, dir string, keep func(string, int) bool) ([]findings.Mutant, findings.MutantCounts, error) {
	return convertFiles(raw, dir, keep, map[string]findings.MutantCounts{})
}

// convertFiles is convert that also adds each file's counts to files, so
// reliab can score a baseline scope.
func convertFiles(raw []byte, dir string, keep func(string, int) bool, files map[string]findings.MutantCounts) ([]findings.Mutant, findings.MutantCounts, error) {
	var c findings.MutantCounts
	var r report
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, c, fmt.Errorf("not a gremlins report: %w", err)
	}
	if r.GoModule == "" {
		return nil, c, fmt.Errorf("not a gremlins report: no go_module")
	}
	var ms []findings.Mutant
	for _, f := range r.Files {
		file := filepath.ToSlash(f.FileName)
		// gremlins run in DIR also mutates the changed files of packages
		// nested under it, against DIR's tests, so it reports them all not
		// covered; each nested package has its own run and report.
		if dir != "" && strings.Contains(file, "/") {
			continue
		}
		if dir != "" && dir != "." {
			file = path.Join(dir, file)
		}
		if !builtHere(file) {
			continue
		}
		for _, m := range f.Mutations {
			if keep != nil && !keep(file, m.Line) {
				continue
			}
			var detail string
			fc := files[file]
			switch m.Status {
			case "KILLED", "TIMED OUT":
				c.Killed++
				fc.Killed++
				files[file] = fc
				continue
			case "LIVED":
				c.Survived++
				fc.Survived++
			case "NOT COVERED":
				c.NotCovered++
				fc.NotCovered++
				detail = "no test covers this line"
			default:
				continue
			}
			files[file] = fc
			ms = append(ms, findings.Mutant{File: file, Description: m.Type, Detail: detail, Line: m.Line, Col: m.Column})
		}
	}
	return ms, c, nil
}

// builtHere reports whether this platform builds file, a path from the
// working directory, by its name and build constraints. A file it cannot
// read counts as built, so a mutant is never dropped by a missing file.
func builtHere(file string) bool {
	ok, err := build.Default.MatchFile(filepath.Dir(filepath.FromSlash(file)), filepath.Base(file))
	return ok || err != nil
}

// changedSince returns the lines changed since base, per file. It runs
// reliab changed-lines, which reliab verify puts first on PATH; tests
// replace it.
var changedSince = func(base string) (map[string]map[int]bool, error) {
	out, err := exec.Command("reliab", "changed-lines", "--json", "--base", base).Output()
	if err != nil {
		return nil, fmt.Errorf("reliab changed-lines --base %s: %w", base, err)
	}
	return parseChanged(out)
}

// parseChanged reads reliab changed-lines --json.
func parseChanged(raw []byte) (map[string]map[int]bool, error) {
	var doc struct {
		Files map[string][][2]int `json:"files"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("reliab changed-lines: %w", err)
	}
	out := map[string]map[int]bool{}
	for file, ranges := range doc.Files {
		lines := map[int]bool{}
		for _, r := range ranges {
			for i := r[0]; i <= r[1]; i++ {
				lines[i] = true
			}
		}
		out[file] = lines
	}
	return out, nil
}
