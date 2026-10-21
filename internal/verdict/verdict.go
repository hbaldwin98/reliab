// Package verdict records the reviewer's judgement on the pre-PR report. A
// pass is bound to evidence: the reviewer's own pre-PR run, in a clean
// worktree of the same repository, of the same tree, agreeing with the
// author's report. Hooks keep the author from running it; this package
// keeps a pass from being recorded without a matching re-run
// (knowledge/decisions/reviewer-verdict.md).
package verdict

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
)

// Tier is the report a verdict is written to.
const Tier = "prepr"

// Options describe one verdict.
type Options struct {
	Root    string // the author's repository root
	Outcome string // report.VerdictPass or report.VerdictFail
	By      string
	Notes   string
	// Worktree is the reviewer's clean worktree, where it ran verify --tier
	// prepr. Required for a pass.
	Worktree string
	Now      time.Time
}

// Record checks the evidence and writes the verdict into the author's
// latest pre-PR report.
func Record(opt Options) (*report.Verdict, error) {
	if opt.Outcome != report.VerdictPass && opt.Outcome != report.VerdictFail {
		return nil, fmt.Errorf("--outcome %q: want pass or fail", opt.Outcome)
	}
	if strings.TrimSpace(opt.By) == "" {
		return nil, errors.New("--by is required, e.g. reliab:reviewer")
	}
	if strings.TrimSpace(opt.Notes) == "" {
		return nil, errors.New("--notes is required: say what was checked and what was found")
	}
	author, err := report.Read(report.LatestPath(opt.Root, Tier))
	if err != nil {
		return nil, fmt.Errorf("no pre-PR report to record a verdict on: %w", err)
	}
	if err := fresh(opt.Root, author); err != nil {
		return nil, fmt.Errorf("the author's pre-PR report: %w", err)
	}
	v := &report.Verdict{
		Outcome: opt.Outcome, By: opt.By, At: report.Now(opt.Now), Notes: opt.Notes,
		TreeHash: author.Run.TreeHash,
	}
	if opt.Worktree == "" {
		if opt.Outcome == report.VerdictPass {
			return nil, errors.New("a pass needs --worktree: the clean worktree where the reviewer ran reliab verify --tier prepr")
		}
	} else {
		rerun, err := Rerun(opt.Root, opt.Worktree)
		if err != nil {
			if opt.Outcome == report.VerdictPass {
				return nil, err
			}
		} else {
			if problems := Compare(author, rerun); len(problems) > 0 && opt.Outcome == report.VerdictPass {
				return nil, fmt.Errorf("cannot record a pass; the re-run does not back the author's report:\n- %s\nRecord --outcome fail with these in --notes", strings.Join(problems, "\n- "))
			}
			v.Head, v.RerunStartedAt = rerun.Run.Head, rerun.Run.StartedAt
		}
	}
	author.Verdict = v
	if err := report.Rewrite(opt.Root, author); err != nil {
		return nil, err
	}
	return v, nil
}

// Rerun reads the reviewer's pre-PR report from its worktree. The worktree
// must be another worktree of root's repository, with no changes, and its
// report must be fresh for it.
func Rerun(root, worktree string) (*report.Report, error) {
	wt, err := filepath.Abs(worktree)
	if err != nil {
		return nil, err
	}
	wtRoot, err := gitx.Root(wt)
	if err != nil {
		return nil, fmt.Errorf("--worktree %s is not a git worktree: %w", worktree, err)
	}
	if same(wtRoot, root) {
		return nil, fmt.Errorf("--worktree %s is the author's own checkout; the reviewer re-runs in a clean worktree of its own", worktree)
	}
	a, errA := gitx.CommonDir(root)
	b, errB := gitx.CommonDir(wtRoot)
	if errA != nil || errB != nil || !same(a, b) {
		return nil, fmt.Errorf("--worktree %s is not a worktree of this repository", worktree)
	}
	if changed, err := gitx.ChangedFiles(wtRoot); err != nil {
		return nil, err
	} else if len(changed) > 0 {
		return nil, fmt.Errorf("--worktree %s is not clean (%s); the re-run must be of a commit, untouched", worktree, strings.Join(changed, ", "))
	}
	rerun, err := report.Read(report.LatestPath(wtRoot, Tier))
	if err != nil {
		return nil, fmt.Errorf("no pre-PR report in --worktree %s (run reliab verify --tier prepr there): %w", worktree, err)
	}
	if err := fresh(wtRoot, rerun); err != nil {
		return nil, fmt.Errorf("the re-run in %s: %w", worktree, err)
	}
	return rerun, nil
}

// Compare lists where the reviewer's re-run fails to back the author's
// report: another tree, an incomplete run, other commands run or skipped,
// or an open finding the author's report does not have.
func Compare(author, rerun *report.Report) []string {
	var out []string
	if rerun.Run.Tier != Tier {
		out = append(out, fmt.Sprintf("the re-run is a %s report, not %s", rerun.Run.Tier, Tier))
	}
	if rerun.Run.TreeHash != author.Run.TreeHash {
		out = append(out, fmt.Sprintf("the re-run saw tree %s but the author's report is for %s; commit every change so the clean worktree has the same tree", rerun.Run.TreeHash, author.Run.TreeHash))
	}
	if !author.Run.Complete {
		out = append(out, "the author's report is incomplete")
	}
	if !rerun.Run.Complete {
		out = append(out, "the re-run is incomplete")
	}
	// Named tests decide what ran, so a re-run with another selection does
	// not back the author's (knowledge/decisions/named-tests.md).
	if !slices.Equal(author.Run.Tests, rerun.Run.Tests) {
		out = append(out, fmt.Sprintf("the tests named differ: author %q, re-run %q; re-run with the author's --tests", author.Run.Tests, rerun.Run.Tests))
	}
	if !slices.Equal(author.Run.MutationTests, rerun.Run.MutationTests) {
		out = append(out, fmt.Sprintf("the mutation tests named differ: author %q, re-run %q; re-run with the author's --mutation-tests", author.Run.MutationTests, rerun.Run.MutationTests))
	}
	if a, b := ran(author), ran(rerun); !slices.Equal(a, b) {
		out = append(out, fmt.Sprintf("the commands that ran differ: author %s, re-run %s", strings.Join(a, ","), strings.Join(b, ",")))
	}
	if a, b := skipped(author), skipped(rerun); !slices.Equal(a, b) {
		out = append(out, fmt.Sprintf("the skips differ: author %s, re-run %s", strings.Join(a, ","), strings.Join(b, ",")))
	}
	have := map[string]bool{}
	for _, f := range author.Findings {
		have[f.ID] = true
	}
	for _, f := range rerun.Findings {
		if f.Status == report.StatusOpen && !have[f.ID] {
			out = append(out, fmt.Sprintf("the re-run found %s, which the author's report does not have", f.ID))
		}
	}
	return out
}

func ran(r *report.Report) []string {
	var ids []string
	for _, c := range r.Run.Commands {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	return ids
}

func skipped(r *report.Report) []string {
	var ids []string
	for _, s := range r.Run.Skipped {
		ids = append(ids, s.ID+":"+s.Reason)
	}
	slices.Sort(ids)
	return ids
}

// fresh checks that the tree at root still hashes to the report's tree.
func fresh(root string, r *report.Report) error {
	p, err := profile.Load(r.ProfileFile(root, profile.DefaultPath))
	if err != nil {
		return err
	}
	h, err := gitx.TreeHash(root, p.TreeExclude())
	if err != nil {
		return err
	}
	if h != r.Run.TreeHash {
		return errors.New("stale: the tree changed after it ran; re-run reliab verify --tier prepr")
	}
	return nil
}

// same reports whether two paths name one directory, whatever their case
// or spelling.
func same(a, b string) bool {
	sa, errA := os.Stat(a)
	sb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(sa, sb)
}
