package guard

import (
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// knowledge/decisions/report-freshness.md: a report whose tree changed while
// it ran vouches for no tree. Undoing the edit afterwards does not make it
// fresh: the stop hook names what changed during the run, and the pre-push
// reminder calls the report stale.
func TestReportWhoseTreeChangedWhileItRanIsStale(t *testing.T) {
	t.Parallel()
	r := open(t, "commands:\n  build: echo edited > src/a/a.txt\n")
	run(t, r, "inner")
	testrepo.Write(t, r.Root, "src/a/a.txt", "a")
	if d := r.Stop(hookio.Input{}); !strings.Contains(d.Block, "src/a/a.txt") {
		t.Errorf("stop after an edit during the run, undone: %+v", d)
	}
	run(t, r, "prepr")
	testrepo.Write(t, r.Root, "src/a/a.txt", "a")
	if d := r.PrePush(); !strings.Contains(d.Context, "stale") {
		t.Errorf("push after an edit during the run, undone: %+v", d)
	}
}
