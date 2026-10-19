// Command reliab-adapter-insta converts a run of insta snapshot tests into
// reliab's findings format, labelling each snapshot that no longer matches
// additive-only or values-changed
// (knowledge/decisions/snapshot-diff-labels.md).
//
//	rm -rf .reliab/out/insta; INSTA_UPDATE=new INSTA_PENDING_DIR="$PWD/.reliab/out/insta" cargo nextest run ...;
//	reliab-adapter-insta -exit $? -junit target/nextest/snapshots/junit.xml .reliab/out/insta .reliab/out/snapshots.json
//
// INSTA_UPDATE=new makes insta write each mismatch as a pending .snap.new
// file, without touching the accepted snapshot; INSTA_PENDING_DIR (in insta
// 1.49; older versions may lack it) puts those files outside the protected snapshot
// directories, mirroring their paths under the workspace. Each pending file
// is compared with the accepted snapshot under -root and becomes one
// finding. Without INSTA_PENDING_DIR, pass the workspace as the pending
// directory: the files sit beside the snapshots, and a stale one from an
// earlier run would be reported again.
//
// With -junit, failing tests that are not snapshot assertions become
// findings too, so a panic elsewhere is not lost. Without it, a failing run
// with no pending snapshot writes nothing and exits 1, and reliab reports
// the command as failed with its log.
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

	"github.com/hbaldwin98/reliab/internal/adapters"
	"github.com/hbaldwin98/reliab/pkg/findings"
	"github.com/hbaldwin98/reliab/pkg/snapdiff"
)

func main() {
	exit := flag.Int("exit", -1, "the test run's exit code")
	junit := flag.String("junit", "", "the test run's JUnit report, to keep failures that are not snapshot assertions")
	root := flag.String("root", ".", "the workspace the accepted snapshots live under")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: reliab-adapter-insta -exit <code> [-junit <junit.xml>] [-root <workspace>] <pending-dir> <findings.json>")
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*exit, *junit, *root, flag.Arg(0), flag.Arg(1)); err != nil {
		fmt.Fprintln(os.Stderr, "reliab-adapter-insta:", err)
		os.Exit(1)
	}
}

// assertion is how insta's panic message starts for a snapshot mismatch.
const assertion = "snapshot assertion"

func run(exit int, junit, root, pending, out string) error {
	fs, err := snapshots(root, pending)
	if err != nil {
		return err
	}
	if junit == "" {
		if len(fs) == 0 && exit != 0 {
			return fmt.Errorf("the run exited %d with no pending snapshot; see its log", exit)
		}
		return findings.Write(out, fs)
	}
	raw, err := os.ReadFile(junit)
	if err != nil {
		return fmt.Errorf("the run exited %d without a JUnit report: %w", exit, err)
	}
	fails, err := adapters.ParseJUnit(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", junit, err)
	}
	asserted := 0
	for _, f := range fails {
		if strings.Contains(f.Message, assertion) {
			asserted++
			continue
		}
		fs = append(fs, f)
	}
	switch {
	case asserted > 0 && len(fs) == 0:
		return fmt.Errorf("%d snapshot assertion(s) failed but %s holds no pending snapshot: run the tests with INSTA_UPDATE=new and INSTA_PENDING_DIR set to it", asserted, pending)
	case len(fs) == 0 && exit != 0:
		return fmt.Errorf("the run exited %d with no failing test or pending snapshot; see its log", exit)
	}
	return findings.Write(out, fs)
}

// snapshots compares each pending snapshot under pending with its accepted
// file under root.
func snapshots(root, pending string) ([]findings.Finding, error) {
	var out []findings.Finding
	err := filepath.WalkDir(pending, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == pending {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() {
			if path != pending && (d.Name() == ".git" || d.Name() == "target" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(pending, path)
		if err != nil {
			return err
		}
		switch {
		case strings.HasSuffix(path, ".snap.new"):
			f, changed, err := compare(filepath.Join(root, strings.TrimSuffix(rel, ".new")), path, strings.TrimSuffix(rel, ".new"))
			if err != nil {
				return err
			}
			if changed {
				out = append(out, f)
			}
		case strings.HasSuffix(path, ".pending-snap"):
			name := filepath.ToSlash(rel)
			out = append(out, findings.Finding{
				Test:     name,
				Location: name,
				Message:  "pending inline snapshot: this adapter labels file snapshots only; review it with insta",
			})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Test < out[j].Test })
	return out, err
}

// compare labels one pending snapshot against its accepted file; name is
// the accepted file's path relative to the workspace. changed is false when
// their contents are equal, which insta does not report as a failure.
func compare(acceptedPath, pendingPath, name string) (f findings.Finding, changed bool, err error) {
	name = filepath.ToSlash(name)
	cur, err := os.ReadFile(pendingPath)
	if err != nil {
		return f, false, err
	}
	var acc []byte
	head := "snapshot changed"
	acc, err = os.ReadFile(acceptedPath)
	if errors.Is(err, fs.ErrNotExist) {
		head, acc, err = "new snapshot, none accepted yet", nil, nil
	}
	if err != nil {
		return f, false, err
	}
	r := snapdiff.Compare(body(string(acc)), body(string(cur)))
	if r.Label == "" {
		return f, false, nil
	}
	return findings.Finding{
		Test:     name,
		Location: name,
		Message:  fmt.Sprintf("%s: %s (+%d -%d)\n%s", head, snapdiff.Describe(r.Label), r.Added, r.Removed, r.Diff),
		Diff:     r.Label,
	}, true, nil
}

// body drops insta's metadata header, which is YAML between two --- lines
// and records where the assertion is, not what it says.
func body(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	if i := strings.Index(s[4:], "\n---\n"); i >= 0 {
		return s[4+i+5:]
	}
	return s
}
