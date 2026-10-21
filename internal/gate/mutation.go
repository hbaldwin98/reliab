package gate

import (
	"fmt"
	"slices"

	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/mutscore"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/verify"
)

// MutationLine is one mutation command's result against what it is held
// to. Fail is true when it falls short or could not be judged.
type MutationLine struct {
	Command string
	Text    string
	Fail    bool
}

// Mutation holds the tier's latest report to the profile's mutation
// thresholds and the baseline file's scopes, re-scoring it from its counts
// so a report written before a threshold was set is still judged. A
// command held to something fails when it scores below it, reports no
// counts, a scope cannot be scored, or it did not run; a command skipped
// as not relevant passes. A person's acknowledgement of a shortfall is
// local and does not pass the gate: the CI host's override is the human
// decision there (knowledge/decisions/mutation-threshold.md).
func Mutation(root string, p *profile.Profile, tier string) ([]MutationLine, error) {
	b, err := mutscore.LoadBaseline(root, p)
	if err != nil {
		return nil, err
	}
	rep, err := report.Read(report.LatestPath(root, tier))
	if err != nil {
		return nil, fmt.Errorf("no readable %s report (run reliab verify --tier %s first): %w", tier, tier, err)
	}
	tree, err := gitx.TreeHash(root, p.TreeExclude())
	if err != nil {
		return nil, err
	}
	var ids []string
	if t, ok := verify.ResolveTier(p, tier); ok {
		ids = t.Run
	} else {
		ids = rep.Run.Only
	}
	var out []MutationLine
	if !rep.FreshFor(tree) {
		out = append(out, MutationLine{Text: fmt.Sprintf("the %s report is stale: it ran on tree %s and the tree is now %s; re-run reliab verify --tier %s", tier, rep.Run.TreeHash, tree, tier), Fail: true})
	}
	for _, id := range ids {
		cmd, ok := p.Commands[id]
		if !mutscore.IsMutation(id) || !ok {
			continue
		}
		held := cmd.Threshold != nil || (b != nil && len(b.Scopes) > 0)
		if !held {
			continue
		}
		out = append(out, judge(id, rep, p, b)...)
	}
	return out, nil
}

func judge(id string, rep *report.Report, p *profile.Profile, b *mutscore.Baseline) []MutationLine {
	i := slices.IndexFunc(rep.Run.Commands, func(c report.CommandResult) bool { return c.ID == id })
	if i < 0 {
		for _, s := range rep.Run.Skipped {
			// The change cannot affect it, so there is no changed code to
			// hold (knowledge/decisions/relevance-skips.md).
			if s.ID == id && s.Reason == report.SkipNotRelevant {
				return []MutationLine{{Command: id, Text: "skipped as not relevant (" + s.Detail + "): nothing to hold"}}
			}
			if s.ID == id {
				return []MutationLine{{Command: id, Text: fmt.Sprintf("skipped (%s): not scored", why(s)), Fail: true}}
			}
		}
		return []MutationLine{{Command: id, Text: "did not run: not scored", Fail: true}}
	}
	c := rep.Run.Commands[i]
	s := mutscore.Compute(c, rep.Findings, p, b)
	if s == nil {
		return []MutationLine{{Command: id, Text: "reported no mutant counts: not scored", Fail: true}}
	}
	var out []MutationLine
	for _, ch := range mutscore.Checks(id, s) {
		what := fmt.Sprintf("threshold %s%%", num(ch.Want))
		if ch.Scope != "" {
			what = fmt.Sprintf("baseline %s %s%%", ch.Scope, num(ch.Want))
		}
		// if, not switch: Go coverage does not instrument case expressions,
		// so gremlins would call their mutants not covered.
		if ch.Unmeasured {
			out = append(out, MutationLine{Command: id, Text: what + ": not scored, the tool reported no per-file counts", Fail: true})
		} else if ch.Counted == 0 {
			out = append(out, MutationLine{Command: id, Text: what + ": no mutants counted, nothing to hold"})
		} else if ch.Below() {
			out = append(out, MutationLine{Command: id, Text: fmt.Sprintf("%s: %.1f%% (%d killed of %d counted), short by %d", what, report.Percent(ch.Killed, ch.Counted), ch.Killed, ch.Counted, ch.Short()), Fail: true})
		} else {
			out = append(out, MutationLine{Command: id, Text: fmt.Sprintf("%s: %.1f%% (%d killed of %d counted), met", what, report.Percent(ch.Killed, ch.Counted), ch.Killed, ch.Counted)})
		}
	}
	if len(out) == 0 {
		// Held only by baseline scopes this run measured nothing in.
		out = append(out, MutationLine{Command: id, Text: "no baseline scope had mutants in this run, nothing to hold"})
	}
	return out
}

func why(s report.Skip) string {
	if s.Detail != "" {
		return s.Reason + ": " + s.Detail
	}
	return s.Reason
}

func num(f float64) string { return fmt.Sprintf("%g", f) }
