// Command reliab-adapter-verify converts a run of Verify snapshot tests
// (.NET) into reliab's findings format, labelling each snapshot that no
// longer matches additive-only or values-changed
// (knowledge/decisions/snapshot-diff-labels.md).
//
//	DiffEngine_Disabled=true dotnet test ... --logger "trx;LogFileName=snapshots.trx" --results-directory .reliab/out;
//	reliab-adapter-verify -exit $? -trx '.reliab/out/snapshots.trx' -root . .reliab/out/snapshots.json
//
// Verify writes each mismatch as X.received.<ext> beside the accepted
// X.verified.<ext>, which it leaves alone. Each received file is labelled
// against its verified file with pkg/snapdiff and joined to the failing
// test that wrote it (Class.Method.received.txt belongs to Method). That
// test keeps its identity and gains the label; a received file no failure
// claims becomes a finding of its own, and failures that are not snapshot
// mismatches stay findings.
//
// Received files sit in the source tree, so one an earlier run left (a
// mutation tool runs the snapshot tests against every mutant) would be
// reported again. Only files modified at or after -since count; it defaults
// to RELIAB_COMMAND_START, which reliab verify sets for each command.
//
// Test reports older than -since are ignored too, since a runner that names
// its reports by time leaves earlier ones beside this run's. A test report
// (-trx or -junit) is required: without one, a failing run
// writes nothing and exits 1, and reliab reports the command as failed.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/hbaldwin98/reliab/pkg/findings"
	"github.com/hbaldwin98/reliab/pkg/snapdiff"
	"github.com/hbaldwin98/reliab/pkg/testresults"
)

func main() {
	exit := flag.Int("exit", -1, "the test run's exit code")
	trx := flag.String("trx", "", "the test run's TRX report (a glob matches several)")
	junit := flag.String("junit", "", "the test run's JUnit report, instead of -trx")
	root := flag.String("root", ".", "the directory to look for received snapshots under")
	since := flag.String("since", os.Getenv("RELIAB_COMMAND_START"), "RFC 3339 time; older received files are ignored (default RELIAB_COMMAND_START)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: reliab-adapter-verify -exit <code> (-trx <file> | -junit <file>) [-root <dir>] [-since <time>] <findings.json>")
	}
	flag.Parse()
	if flag.NArg() != 1 || (*trx == "") == (*junit == "") {
		flag.Usage()
		os.Exit(2)
	}
	var after time.Time
	if *since != "" {
		t, err := time.Parse(time.RFC3339Nano, *since)
		if err != nil {
			fmt.Fprintln(os.Stderr, "reliab-adapter-verify: -since:", err)
			os.Exit(2)
		}
		after = t
	}
	if err := run(*exit, *trx, *junit, *root, after, flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "reliab-adapter-verify:", err)
		os.Exit(1)
	}
}

func run(exit int, trx, junit, root string, after time.Time, out string) error {
	fails, err := testFailures(trx, junit, after)
	if err != nil {
		return fmt.Errorf("the run exited %d: %w", exit, err)
	}
	pend, err := received(root, after)
	if err != nil {
		return err
	}
	fs := join(fails, pend)
	if len(fs) == 0 && exit != 0 {
		return fmt.Errorf("the run exited %d with no failing test or received snapshot; see its log", exit)
	}
	return findings.Write(out, fs)
}

// testFailures reads the failing tests from every report the pattern
// matches that was written at or after after: a runner that names its
// reports by time leaves earlier runs' reports beside this one's.
func testFailures(trx, junit string, after time.Time) ([]findings.Finding, error) {
	pattern, parse := trx, testresults.ParseTRX
	if junit != "" {
		pattern, parse = junit, testresults.ParseJUnit
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range matches {
		if info, err := os.Stat(f); err == nil && !info.ModTime().Before(after) {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no test report %s written by this run", pattern)
	}
	sort.Strings(files)
	var out []findings.Finding
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		got, err := parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out = append(out, got...)
	}
	return out, nil
}

// snapshot is one received file and the verified file it would replace,
// named relative to the root.
type snapshot struct {
	received, verified, name string
}

// skipDirs are never searched: build output and dependencies.
var skipDirs = map[string]bool{".git": true, ".reliab": true, "bin": true, "obj": true, "node_modules": true}

// received finds the received snapshots under root modified at or after
// after.
func received(root string, after time.Time) ([]snapshot, error) {
	var out []snapshot
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == root {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		base := d.Name()
		i := strings.LastIndex(base, ".received.")
		if i < 0 {
			return nil
		}
		if !after.IsZero() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.ModTime().Before(after) {
				return nil
			}
		}
		verified := filepath.Join(filepath.Dir(path), base[:i]+".verified."+base[i+len(".received."):])
		name, err := filepath.Rel(root, verified)
		if err != nil {
			return err
		}
		out = append(out, snapshot{received: path, verified: verified, name: filepath.ToSlash(name)})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, err
}

// join labels each received snapshot and attaches it to the failing test
// that wrote it: the failure whose last name segment is a dot-separated part
// of the received file's name. A snapshot no failure claims is a finding of
// its own.
func join(fails []findings.Finding, snaps []snapshot) []findings.Finding {
	out := append([]findings.Finding(nil), fails...)
	claimed := make([]bool, len(out))
	for _, s := range snaps {
		f, changed, err := label(s)
		if err != nil {
			f = findings.Finding{Test: s.name, Location: s.name, Message: err.Error()}
		} else if !changed {
			continue
		}
		i := owner(out, claimed, filepath.Base(s.received))
		if i < 0 {
			out = append(out, f)
			claimed = append(claimed, true)
			continue
		}
		claimed[i] = true
		out[i].Location, out[i].Diff = f.Location, f.Diff
		out[i].Message = strings.TrimSpace(f.Message + "\n\n" + out[i].Message)
	}
	return out
}

// label compares one received snapshot with its verified file. changed is
// false when their texts are equal.
func label(s snapshot) (findings.Finding, bool, error) {
	r, err := snapdiff.CompareFiles(s.verified, s.received)
	if err != nil {
		return findings.Finding{}, false, err
	}
	if r.Label == "" {
		return findings.Finding{}, false, nil
	}
	f := findings.Finding{Test: s.name, Location: s.name}
	head := "snapshot changed"
	if r.New {
		head = "new snapshot, none accepted yet"
	}
	if r.Binary {
		f.Message = head + ": binary snapshot, not labelled; compare " + filepath.Base(s.received) + " with " + filepath.Base(s.verified)
		return f, true, nil
	}
	f.Message = fmt.Sprintf("%s: %s (+%d -%d)\n%s", head, snapdiff.Describe(r.Label), r.Added, r.Removed, r.Diff)
	f.Diff = r.Label
	return f, true, nil
}

// owner returns the index of the unclaimed failure whose test wrote the
// received file base, or -1.
func owner(fails []findings.Finding, claimed []bool, base string) int {
	parts := map[string]bool{}
	for _, s := range strings.Split(base, ".") {
		parts[s] = true
	}
	for i, f := range fails {
		if !claimed[i] && parts[lastSegment(f.Test)] {
			return i
		}
	}
	return -1
}

// lastSegment is the last identifier in a test name, before any
// parameters: Ns.Class.Method(1) -> Method.
func lastSegment(name string) string {
	if i := strings.IndexAny(name, "(["); i >= 0 {
		name = name[:i]
	}
	fields := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
