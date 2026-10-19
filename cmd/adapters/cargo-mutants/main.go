// Command reliab-adapter-cargo-mutants converts a cargo-mutants output
// directory into reliab's findings format
// (knowledge/decisions/external-adapters.md).
//
//	rm -rf out/mutants.out; cargo mutants --in-diff d.diff -o out; reliab-adapter-cargo-mutants -exit $? out/mutants.out findings.json
//
// Missed mutants become findings, each with its diff inlined; timeouts count
// as caught, and the findings file counts caught and missed mutants
// (unviable ones were never tested). A baseline that fails (the unmutated tree) is one finding.
// -exit is cargo-mutants' exit code: when it is 0 and there is no
// outcomes.json, cargo-mutants found nothing to mutate (a diff without Rust
// changes) and the findings file is empty. Remove the output directory
// before each run, since cargo-mutants leaves the previous one in place when
// it has nothing to mutate. With no outcomes to read otherwise, this exits 1
// and writes nothing, and reliab reports the command as failed.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hbaldwin98/reliab/pkg/findings"
)

func main() {
	exit := flag.Int("exit", -1, "cargo-mutants' exit code")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: reliab-adapter-cargo-mutants -exit <code> <mutants.out> <findings.json>")
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*exit, flag.Arg(0), flag.Arg(1)); err != nil {
		fmt.Fprintln(os.Stderr, "reliab-adapter-cargo-mutants:", err)
		os.Exit(1)
	}
}

func run(exit int, dir, out string) error {
	raw, err := os.ReadFile(filepath.Join(dir, "outcomes.json"))
	if errors.Is(err, fs.ErrNotExist) && exit == 0 {
		return findings.Write(out, nil)
	}
	if err != nil {
		return fmt.Errorf("cargo-mutants exited %d without outcomes: %w", exit, err)
	}
	fs, counts, err := convert(raw, dir)
	if err != nil {
		return err
	}
	return findings.WriteMutants(out, fs, &counts)
}

type outcomes struct {
	Outcomes []struct {
		Scenario json.RawMessage `json:"scenario"`
		Summary  string          `json:"summary"`
		LogPath  string          `json:"log_path"`
		DiffPath *string         `json:"diff_path"`
	} `json:"outcomes"`
	Version string `json:"cargo_mutants_version"`
}

type scenario struct {
	Mutant struct {
		Name string `json:"name"`
		File string `json:"file"`
		Span struct {
			Start struct{ Line, Column int } `json:"start"`
		} `json:"span"`
	} `json:"Mutant"`
}

// namePrefix is the "file:line:col: " that starts a mutant name.
var namePrefix = regexp.MustCompile(`^.*?:\d+:\d+: `)

// convert reads outcomes.json and counts the mutants it tested; dir is its
// directory, against which log and diff paths resolve.
func convert(raw []byte, dir string) ([]findings.Finding, findings.MutantCounts, error) {
	var c findings.MutantCounts
	var doc outcomes
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, c, fmt.Errorf("not a cargo-mutants outcomes.json: %w", err)
	}
	if doc.Version == "" {
		return nil, c, fmt.Errorf("not a cargo-mutants outcomes.json: no cargo_mutants_version")
	}
	var out []findings.Finding
	var ms []findings.Mutant
	for _, o := range doc.Outcomes {
		var name string
		if json.Unmarshal(o.Scenario, &name) == nil {
			if name == "Baseline" && o.Summary != "Success" {
				out = append(out, findings.Finding{
					Test:    "baseline",
					Message: fmt.Sprintf("the unmutated tree did not pass (%s); no mutant was tested. Log: %s", o.Summary, filepath.ToSlash(filepath.Join(dir, o.LogPath))),
				})
			}
			continue
		}
		switch o.Summary {
		case "CaughtMutant", "Timeout":
			c.Killed++
			continue
		case "MissedMutant":
			c.Survived++
		default:
			continue
		}
		var s scenario
		if err := json.Unmarshal(o.Scenario, &s); err != nil || s.Mutant.File == "" {
			return nil, c, fmt.Errorf("unreadable mutant scenario: %s", o.Scenario)
		}
		m := findings.Mutant{
			File:        filepath.ToSlash(s.Mutant.File),
			Description: namePrefix.ReplaceAllString(s.Mutant.Name, ""),
			Line:        s.Mutant.Span.Start.Line,
			Col:         s.Mutant.Span.Start.Column,
		}
		if o.DiffPath != nil {
			if diff, err := os.ReadFile(filepath.Join(dir, *o.DiffPath)); err == nil {
				m.Detail = strings.TrimSpace(string(diff))
			}
		}
		ms = append(ms, m)
	}
	return append(out, findings.Mutants(ms)...), c, nil
}
