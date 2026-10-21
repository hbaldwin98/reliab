package report

import (
	"fmt"
	"time"

	"github.com/hbaldwin98/reliab/internal/gitx"
)

// BranchHistory returns the history reports of the branch checked out at
// root, oldest first: runs whose HEAD lies between the merge base with the
// base ref and the current HEAD, started no later than before (the zero
// time sets no limit). .reliab/ is shared by every branch of a checkout,
// so runs from another branch are left out. The PR summary's fixed list
// reads these runs, and classifying a fixed finding writes to one of them,
// so both choose them here (knowledge/decisions/pr-evidence-summary.md).
//
// The base is explicit (a flag) if set, else profileBase, else the first
// of gitx.BaseFallbacks. It is returned even when its merge base cannot be
// found, with the error.
func BranchHistory(root, explicit, profileBase string, before time.Time) ([]*Report, string, error) {
	base, err := gitx.ResolveBase(root, explicit, profileBase)
	if err != nil {
		return nil, "", err
	}
	mb, err := gitx.MergeBase(root, base)
	if err != nil {
		return nil, base, fmt.Errorf("no merge base with %s: %v", base, err)
	}
	head := gitx.Head(root)
	onBranch := map[string]bool{}
	inBranch := func(h string) bool {
		if h == "" {
			return false
		}
		if v, ok := onBranch[h]; ok {
			return v
		}
		v := gitx.IsAncestor(root, mb, h) && gitx.IsAncestor(root, h, head)
		onBranch[h] = v
		return v
	}
	var out []*Report
	for _, r := range History(root) {
		if !before.IsZero() && startTime(r).After(before) {
			continue
		}
		if inBranch(r.Run.Head) {
			out = append(out, r)
		}
	}
	return out, base, nil
}
