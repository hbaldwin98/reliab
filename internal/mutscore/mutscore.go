// Package mutscore scores mutation commands, holds them to the profile's
// threshold and the baseline file's scopes, and turns a shortfall into a
// below-threshold finding (knowledge/decisions/mutation-threshold.md).
//
// The rule: a command's score is killed over counted, where counted is
// killed, survived, and not covered, less the survivors a person confirmed
// equivalent. Timeouts count as killed. Mutants the tool did not test (did
// not build, skipped) are not counted. A requirement-silent survivor
// counts: nobody has pinned its behavior. A survivor classified equivalent
// counts until a person acknowledges it.
package mutscore

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/hbaldwin98/reliab/internal/adapters"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
)

// IsMutation reports whether a command id is a mutation command.
func IsMutation(id string) bool { return strings.HasPrefix(id, "mutation_") }

// Compute scores one mutation command from its counts and the report's
// findings. It returns nil when the command reported no counts.
func Compute(c report.CommandResult, findings []report.Finding, p *profile.Profile, b *Baseline) *report.Score {
	if c.Mutants == nil {
		return nil
	}
	m := c.Mutants
	s := &report.Score{Killed: m.Killed}
	confirmed := map[string]int{}
	for _, f := range findings {
		if f.Command != c.ID || f.Kind != report.KindEquivalentMutant {
			continue
		}
		if f.Status == report.StatusAcknowledged {
			s.Equivalent++
			confirmed[fileOf(f.Location)]++
		} else {
			s.PendingEquivalent++
		}
	}
	s.Counted = max(m.Killed, m.Killed+m.Survived+m.NotCovered-s.Equivalent)
	if cmd, ok := p.Commands[c.ID]; ok && cmd.Threshold != nil {
		t := *cmd.Threshold
		s.Threshold = &t
	}
	if b == nil || s.Counted == 0 {
		return s
	}
	for _, sc := range b.Scopes {
		if c.MutantFiles == nil {
			s.Baselines = append(s.Baselines, report.ScopeScore{Scope: sc.Path, Baseline: sc.Score, Unmeasured: true})
			continue
		}
		killed, counted := inScope(sc.Path, c.MutantFiles, confirmed)
		if counted > 0 {
			s.Baselines = append(s.Baselines, report.ScopeScore{Scope: sc.Path, Baseline: sc.Score, Killed: killed, Counted: counted})
		}
	}
	return s
}

// inScope sums the per-file counts of the files a scope glob matches, less
// the confirmed equivalents in them.
func inScope(glob string, files map[string]report.FileCounts, confirmed map[string]int) (killed, counted int) {
	for f, c := range files {
		if ok, _ := doublestar.Match(glob, f); !ok {
			continue
		}
		killed += c.Killed
		counted += max(c.Killed, c.Killed+c.Survived+c.NotCovered-confirmed[f])
	}
	return killed, counted
}

// Check is one comparison of a score with what it must reach: the
// threshold (Scope empty) or one baseline scope.
type Check struct {
	Command string
	Scope   string
	Want    float64
	Killed  int
	Counted int
	// Unmeasured: the scope could not be scored (no per-file counts).
	Unmeasured bool
}

// Below reports whether the check falls short.
func (c Check) Below() bool { return report.Below(c.Killed, c.Counted, c.Want) }

// Short is how many more mutants must be killed to reach Want.
func (c Check) Short() int {
	// The epsilon keeps an exact product such as 80% of 10 from rounding up.
	return max(0, int(math.Ceil(c.Want*float64(c.Counted)/100-1e-9))-c.Killed)
}

// Checks lists what a command's score is held to.
func Checks(id string, s *report.Score) []Check {
	if s == nil {
		return nil
	}
	var out []Check
	if s.Threshold != nil {
		out = append(out, Check{Command: id, Want: *s.Threshold, Killed: s.Killed, Counted: s.Counted})
	}
	for _, b := range s.Baselines {
		out = append(out, Check{Command: id, Scope: b.Scope, Want: b.Baseline, Killed: b.Killed, Counted: b.Counted, Unmeasured: b.Unmeasured})
	}
	return out
}

// Apply scores every mutation command in rep and replaces its
// below-threshold findings with one per check that falls short. A
// shortfall with the same numbers keeps a person's acknowledgement; new
// numbers are a new finding.
func Apply(rep *report.Report, p *profile.Profile, b *Baseline) {
	acked := map[string]*report.Acknowledged{}
	kept := make([]report.Finding, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		if f.Kind == report.KindBelowThreshold {
			if f.Acknowledged != nil {
				acked[f.ID] = f.Acknowledged
			}
			continue
		}
		kept = append(kept, f)
	}
	var short []report.Finding
	for i := range rep.Run.Commands {
		c := &rep.Run.Commands[i]
		if !IsMutation(c.ID) {
			continue
		}
		c.Score = Compute(*c, kept, p, b)
		for _, ch := range Checks(c.ID, c.Score) {
			if !ch.Below() {
				continue
			}
			f := Shortfall(ch, *c, kept)
			if a := acked[f.ID]; a != nil {
				f.Acknowledged, f.Status = a, report.StatusAcknowledged
			}
			short = append(short, f)
		}
	}
	rep.Findings = append(kept, short...)
}

// shownSurvivors is how many survivors a shortfall's message names before
// "and N more"; the full list is the survivor findings themselves (the
// owner's call of 2026-10-19, knowledge/decisions/mutation-threshold.md).
const shownSurvivors = 10

// Shortfall is the finding for a check that falls short. Its test carries
// the numbers, so its id changes whenever they do.
func Shortfall(ch Check, c report.CommandResult, findings []report.Finding) report.Finding {
	pct := report.Percent(ch.Killed, ch.Counted)
	var test string
	if ch.Scope == "" {
		test = fmt.Sprintf("mutation score %.1f%% below the threshold %s%% (%d killed of %d counted)", pct, num(ch.Want), ch.Killed, ch.Counted)
	} else {
		test = fmt.Sprintf("mutation score in %s %.1f%% below its baseline %s%% (%d killed of %d counted)", ch.Scope, pct, num(ch.Want), ch.Killed, ch.Counted)
	}
	var survivors []string
	for _, f := range Survivors(ch, findings) {
		survivors = append(survivors, f.ID+" ["+f.Kind+"]")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Short by %d: that many more kills reach %s%%; a survivor a person confirms equivalent leaves the count instead.\n", ch.Short(), num(ch.Want))
	if ch.Scope == "" && c.Mutants != nil {
		fmt.Fprintf(&b, "Counted: %d killed, %d survived, %d not covered", c.Mutants.Killed, c.Mutants.Survived, c.Mutants.NotCovered)
		if s := c.Score; s != nil && s.Equivalent > 0 {
			fmt.Fprintf(&b, ", less %d confirmed equivalent", s.Equivalent)
		}
		b.WriteString(".\n")
	}
	b.WriteString("This finding is its survivors' sum, not work of its own: it resolves as they are handled. " +
		"Handle each per reliab report route: kill one only when its behavior is intended and the test cites where; " +
		"nothing states it: requirement-silent; no test can kill it: equivalent-mutant. Never write a test only to raise the score.\n")
	if len(survivors) > 0 {
		b.WriteString("Survivors counted: " + list(survivors, shownSurvivors))
	} else {
		b.WriteString("No survivor findings are counted here; the shortfall is in the tool's counts alone.")
	}
	return report.Finding{
		ID: adapters.ID(c.ID, test), Kind: report.KindBelowThreshold, Command: c.ID,
		Test: test, Message: b.String(), Status: report.StatusOpen,
	}
}

// CheckOf finds the check a below-threshold finding in r was made for: the
// threshold or the baseline scope whose shortfall has its id.
func CheckOf(r *report.Report, f report.Finding) (Check, bool) {
	for _, c := range r.Run.Commands {
		if c.ID != f.Command {
			continue
		}
		for _, ch := range Checks(c.ID, c.Score) {
			if ch.Below() && Shortfall(ch, c, r.Findings).ID == f.ID {
				return ch, true
			}
		}
	}
	return Check{}, false
}

// Survivors are the findings a check counts against its score: the
// command's survived and uncovered mutants, in the check's scope, less
// those a person confirmed equivalent.
func Survivors(ch Check, findings []report.Finding) []report.Finding {
	var out []report.Finding
	for _, f := range findings {
		if f.Command != ch.Command || f.Kind == report.KindBelowThreshold || f.Test == "" {
			continue
		}
		if f.Kind == report.KindEquivalentMutant && f.Status == report.StatusAcknowledged {
			continue
		}
		if ch.Scope != "" {
			if ok, _ := doublestar.Match(ch.Scope, fileOf(f.Location)); !ok {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

// lineCol is a location's trailing :line or :line:col.
var lineCol = regexp.MustCompile(`(:\d+){1,2}$`)

// fileOf is the file of a finding location such as internal/x.go:12:3.
func fileOf(loc string) string { return lineCol.ReplaceAllString(loc, "") }

// num formats a percentage as the profile wrote it: 80, not 80.000000.
func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func list(items []string, n int) string {
	if len(items) > n {
		return strings.Join(items[:n], ", ") + fmt.Sprintf(" and %d more", len(items)-n)
	}
	return strings.Join(items, ", ")
}
