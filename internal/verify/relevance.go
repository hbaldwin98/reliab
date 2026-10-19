package verify

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/profile"
)

// relevance judges commands that declare relevant_when against the files
// the change touched since the base, so a layer the change cannot affect is
// skipped with the reason (knowledge/decisions/relevance-skips.md, R8).
type relevance struct {
	// notJudged says why every command runs whatever it declares: the
	// nightly tier, an on-demand run, or no base. Empty when judged.
	notJudged string
	base      string
	changed   []string
	// paths ranks the matching file a reason names: product first.
	paths profile.Paths
}

// relevanceTiers are the tiers that skip a command the change cannot
// affect. Nightly runs everything; on-demand runs what was asked for.
var relevanceTiers = []string{"inner", "prepr"}

// judgeRelevance decides whether this run judges relevance and, if so,
// lists the files changed since the base. base is the run's resolved base,
// or empty when no command needed one.
func judgeRelevance(root string, opt Options, prof *profile.Profile, tier Tier, base string) relevance {
	declared := false
	for _, id := range tier.Run {
		if len(prof.Commands[id].RelevantWhen) > 0 {
			declared = true
		}
	}
	if !declared {
		return relevance{}
	}
	if len(opt.Only) > 0 {
		return relevance{notJudged: "asked for by --only, which never skips a command as not relevant"}
	}
	if !slices.Contains(relevanceTiers, tier.Name) {
		return relevance{notJudged: "the " + tier.Name + " tier never skips a command as not relevant"}
	}
	base, changed, err := changedSinceBase(root, opt, prof, base)
	if err != nil {
		return relevance{notJudged: "relevance not judged, so it ran: " + err.Error()}
	}
	return relevance{base: base, changed: changed, paths: prof.Paths}
}

// changedSinceBase resolves the base unless the run already has one, and
// lists the files changed since it.
func changedSinceBase(root string, opt Options, prof *profile.Profile, base string) (string, []string, error) {
	if base == "" {
		var err error
		if base, err = gitx.ResolveBase(root, opt.Base, prof.Base); err != nil {
			return "", nil, err
		}
	}
	changed, err := gitx.ChangedSince(root, base)
	return base, changed, err
}

// check returns why a command with relevant_when runs (ran), or why it is
// skipped (skip, non-empty). A command without relevant_when always runs
// and says nothing.
func (r relevance) check(c profile.Command) (ran, skip string) {
	if len(c.RelevantWhen) == 0 {
		return "", ""
	}
	if r.notJudged != "" {
		return r.notJudged, ""
	}
	// The reason names the matching file most likely to have made the
	// command relevant: product code over a test file or a seed.
	best, glob := "", ""
	for _, f := range r.changed {
		for _, g := range c.RelevantWhen {
			if ok, _ := doublestar.Match(g, f); ok {
				if best == "" || r.rank(f) < r.rank(best) {
					best, glob = f, g
				}
				break
			}
		}
	}
	if best != "" {
		return fmt.Sprintf("%s changed since %s and matches %s", best, r.base, glob), ""
	}
	globs := strings.Join(c.RelevantWhen, ", ")
	if len(r.changed) == 0 {
		return "", fmt.Sprintf("nothing changed since %s, and it is relevant only to %s", r.base, globs)
	}
	return "", fmt.Sprintf("the change since %s touches only %s; it is relevant only to %s", r.base, few(r.changed, 3), globs)
}

// rank orders changed files for a reason: 0 product, 1 tests, 2 anything
// else. A file the test paths match is a test even when the product paths
// match it too, as everywhere else in reliab.
func (r relevance) rank(f string) int {
	match := func(globs []string) bool {
		for _, g := range globs {
			if ok, _ := doublestar.Match(g, f); ok {
				return true
			}
		}
		return false
	}
	if match(r.paths.Tests) {
		return 1
	}
	if match(r.paths.Product) && !match(r.paths.Ignore) {
		return 0
	}
	return 2
}

// few lists up to n items, then how many more.
func few(items []string, n int) string {
	if len(items) > n {
		return strings.Join(items[:n], ", ") + fmt.Sprintf(" and %d more", len(items)-n)
	}
	return strings.Join(items, ", ")
}
