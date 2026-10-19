package verdict

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/verify"
)

func prepr(t *testing.T, root string) *report.Report {
	t.Helper()
	rep, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// reviewed returns an author checkout with a prepr report and a clean
// worktree of its HEAD, named review, where prepr ran too.
func reviewed(t *testing.T, extra string) (root, wt string) {
	t.Helper()
	root = testrepo.New(t, extra)
	prepr(t, root)
	wt = filepath.Join(t.TempDir(), "review")
	testrepo.Git(t, root, "worktree", "add", "-q", "--detach", wt, "HEAD")
	prepr(t, wt)
	return root, wt
}

func record(root, outcome, wt string) (*report.Verdict, error) {
	return Record(Options{Root: root, Outcome: outcome, By: "reliab:reviewer", Notes: "re-ran prepr; citations present", Worktree: wt, Now: time.Now()})
}

// knowledge/decisions/reviewer-verdict.md: a pass is bound to the tree the
// reviewer re-ran in a clean worktree, and lands on the author's report.
func TestPassBoundToRerun(t *testing.T) {
	root, wt := reviewed(t, "commands:\n  build: \"true\"\n")
	v, err := record(root, report.VerdictPass, wt)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := report.Read(report.LatestPath(root, Tier))
	if err != nil {
		t.Fatal(err)
	}
	rerun, err := report.Read(report.LatestPath(wt, Tier))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict == nil || *rep.Verdict != *v {
		t.Fatalf("report verdict = %+v, want %+v", rep.Verdict, v)
	}
	if v.TreeHash != rep.Run.TreeHash || v.Head != rerun.Run.Head || v.RerunStartedAt != rerun.Run.StartedAt || v.Head == "" {
		t.Errorf("verdict = %+v, report tree %s, re-run %+v", v, rep.Run.TreeHash, rerun.Run)
	}
	// knowledge/decisions/report-storage.md: history is the durable record,
	// so it keeps the review after a later run replaces the latest report.
	hist, err := report.Read(report.HistoryPath(root, rep))
	if err != nil {
		t.Fatal(err)
	}
	if hist.Verdict == nil || *hist.Verdict != *v {
		t.Errorf("history verdict = %+v, want %+v", hist.Verdict, v)
	}
}

// knowledge/decisions/reviewer-verdict.md: a pass needs another worktree of
// the same repository, clean, whose fresh prepr report matches the author's
// tree and finds nothing the author's report lacks; a fail is recorded
// instead.
func TestPassRefusals(t *testing.T) {
	t.Run("no worktree", func(t *testing.T) {
		root, _ := reviewed(t, "commands:\n  build: \"true\"\n")
		if _, err := record(root, report.VerdictPass, ""); err == nil || !strings.Contains(err.Error(), "--worktree") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("the author's own checkout", func(t *testing.T) {
		root, _ := reviewed(t, "commands:\n  build: \"true\"\n")
		if _, err := record(root, report.VerdictPass, root); err == nil || !strings.Contains(err.Error(), "own checkout") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("another repository", func(t *testing.T) {
		root, _ := reviewed(t, "commands:\n  build: \"true\"\n")
		other, _ := reviewed(t, "commands:\n  build: \"true\"\n")
		if _, err := record(root, report.VerdictPass, other); err == nil || !strings.Contains(err.Error(), "not a worktree of this repository") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("dirty worktree", func(t *testing.T) {
		root, wt := reviewed(t, "commands:\n  build: \"true\"\n")
		testrepo.Write(t, wt, "src/a/a.txt", "reviewer edit")
		if _, err := record(root, report.VerdictPass, wt); err == nil || !strings.Contains(err.Error(), "not clean") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("uncommitted author change", func(t *testing.T) {
		root := testrepo.New(t, "commands:\n  build: \"true\"\n")
		testrepo.Write(t, root, "src/a/a.txt", "not committed")
		prepr(t, root)
		wt := filepath.Join(t.TempDir(), "review")
		testrepo.Git(t, root, "worktree", "add", "-q", "--detach", wt, "HEAD")
		prepr(t, wt)
		if _, err := record(root, report.VerdictPass, wt); err == nil || !strings.Contains(err.Error(), "commit every change") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("stale author report", func(t *testing.T) {
		root, wt := reviewed(t, "commands:\n  build: \"true\"\n")
		testrepo.Write(t, root, "src/a/a.txt", "edited after prepr")
		if _, err := record(root, report.VerdictPass, wt); err == nil || !strings.Contains(err.Error(), "stale") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("the re-run finds what the author's report lacks", func(t *testing.T) {
		// Fails only in a directory named review: the reviewer's worktree.
		root, wt := reviewed(t, "commands:\n  build: '[ \"$(basename \"$PWD\")\" != review ]'\n")
		_, err := record(root, report.VerdictPass, wt)
		if err == nil || !strings.Contains(err.Error(), "F-build") {
			t.Fatalf("err = %v", err)
		}
		rep, _ := report.Read(report.LatestPath(root, Tier))
		if rep.Verdict != nil {
			t.Fatalf("refused pass left a verdict: %+v", rep.Verdict)
		}
		// The reviewer records what it found instead.
		v, err := record(root, report.VerdictFail, wt)
		if err != nil || v.Outcome != report.VerdictFail || v.RerunStartedAt == "" {
			t.Fatalf("fail verdict = %+v, %v", v, err)
		}
	})
}

// knowledge/decisions/reviewer-verdict.md: outcome is pass or fail, --by and
// --notes are required, the author's pre-PR report must exist, and a fail
// needs no re-run.
func TestRecordRejectsBadInput(t *testing.T) {
	root, wt := reviewed(t, "commands:\n  build: \"true\"\n")
	for name, opt := range map[string]Options{
		"outcome":  {Root: root, Outcome: "ok", By: "r", Notes: "n", Worktree: wt},
		"by":       {Root: root, Outcome: report.VerdictPass, By: " ", Notes: "n", Worktree: wt},
		"notes":    {Root: root, Outcome: report.VerdictPass, By: "r", Notes: "", Worktree: wt},
		"no prepr": {Root: testrepo.New(t, "commands:\n  build: \"true\"\n"), Outcome: report.VerdictFail, By: "r", Notes: "n"},
	} {
		if _, err := Record(opt); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	// A fail needs no re-run: it lets nothing through.
	if v, err := Record(Options{Root: root, Outcome: report.VerdictFail, By: "r", Notes: "a test asserts nothing", Now: time.Now()}); err != nil || v.RerunStartedAt != "" {
		t.Fatalf("fail without a worktree: %+v, %v", v, err)
	}
}

// knowledge/decisions/reviewer-verdict.md: the re-run backs the author's
// report only with the same tree, both complete, the same commands run and
// skipped, and no open finding the author's report lacks.
func TestCompare(t *testing.T) {
	base := func() *report.Report {
		return &report.Report{Run: report.Run{
			Tier: Tier, Complete: true, TreeHash: "t1",
			Commands: []report.CommandResult{{ID: "build"}, {ID: "unit"}},
			Skipped:  []report.Skip{{ID: "api_fuzz", Reason: report.SkipNotApplicable}},
		}, Findings: []report.Finding{{ID: "F-unit-1", Command: "unit", Status: report.StatusAcknowledged}}}
	}
	a, b := base(), base()
	// The re-run has no classifications or acks: an acknowledged finding is open there.
	b.Findings[0].Status = report.StatusOpen
	if p := Compare(a, b); len(p) != 0 {
		t.Fatalf("matching reports: %v", p)
	}
	b.Run.Commands = b.Run.Commands[:1]
	b.Run.Skipped = append(b.Run.Skipped, report.Skip{ID: "unit", Reason: report.SkipBudget})
	b.Run.Complete = false
	b.Run.TreeHash = "t2"
	p := strings.Join(Compare(a, b), "\n")
	for _, want := range []string{"commands that ran differ", "skips differ", "re-run is incomplete", "re-run saw tree t2"} {
		if !strings.Contains(p, want) {
			t.Errorf("problems lack %q:\n%s", want, p)
		}
	}
}
