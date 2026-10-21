package verify

import (
	"testing"

	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// knowledge/decisions/report-freshness.md: a report vouches for the tree it
// ran on only if the tree did not change while it ran. An edit made during
// a run, even one undone afterwards, leaves a report that no tree makes
// fresh.
func TestTreeChangedDuringRunMakesTheReportStale(t *testing.T) {
	root := repo(t, "commands:\n  unit: echo edited > src/a/a.txt\ntiers:\n  inner: { run: [unit] }\n")
	start, err := gitx.TreeHash(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	rep := run(t, Options{Root: root, Tier: "inner"})
	if rep.Run.TreeHash != start || rep.Run.TreeHashEnd == "" || rep.Run.TreeHashEnd == start {
		t.Fatalf("treeHash %s, treeHashEnd %q; want the start tree %s and the edited one", rep.Run.TreeHash, rep.Run.TreeHashEnd, start)
	}
	// The edit undone: the tree is the one the run started on, and the
	// report is still stale.
	testrepo.Write(t, root, "src/a/a.txt", "a")
	if now, _ := gitx.TreeHash(root, nil); now != start || rep.FreshFor(now) {
		t.Errorf("after undoing the edit: tree %s (start %s), fresh %v; want stale", now, start, rep.FreshFor(now))
	}
}

// A run whose tree did not change records no end hash and is fresh for its
// tree.
func TestUnchangedTreeIsFresh(t *testing.T) {
	root := repo(t, "commands:\n  unit: \"true\"\ntiers:\n  inner: { run: [unit] }\n")
	rep := run(t, Options{Root: root, Tier: "inner"})
	now, err := gitx.TreeHash(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Run.TreeHashEnd != "" || !rep.FreshFor(now) {
		t.Errorf("treeHashEnd %q, fresh %v; want none and fresh", rep.Run.TreeHashEnd, rep.FreshFor(now))
	}
}
