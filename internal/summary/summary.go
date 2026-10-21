// Package summary turns a tier's latest report into the evidence section of
// a PR description: what ran, what was skipped and why, mutation results,
// findings fixed and still open, and the questions only a person can
// answer. It states what the reports say and nothing else
// (knowledge/decisions/pr-evidence-summary.md).
package summary

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hbaldwin98/reliab/internal/gate"
	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/verify"
)

// Formats lists the output formats.
var Formats = []string{"markdown", "json"}

// Summary is what the report says, arranged for a reviewer. Its JSON form
// is the --format json output.
type Summary struct {
	Tier          string  `json:"tier"`
	Profile       string  `json:"profile"`
	Tool          string  `json:"tool"`
	StartedAt     string  `json:"startedAt"`
	ElapsedSec    float64 `json:"elapsedSec"`
	BudgetSec     int     `json:"budgetSec"`
	Complete      bool    `json:"complete"`
	TreeHash      string  `json:"treeHash"`
	Head          string  `json:"head,omitempty"`
	Seed          string  `json:"seed,omitempty"`
	PropertyCases int     `json:"propertyCases,omitempty"`
	// Tests and MutationTests are the selections named for the run
	// (knowledge/decisions/named-tests.md), so a reviewer checks them
	// against the diff.
	Tests         []string `json:"tests,omitempty"`
	MutationTests []string `json:"mutationTests,omitempty"`
	// Fresh is whether the working tree still hashes to the report's tree.
	// FreshError says why it could not be checked.
	Fresh       bool   `json:"fresh"`
	CurrentTree string `json:"currentTree,omitempty"`
	FreshError  string `json:"freshError,omitempty"`

	Ran     []Ran         `json:"ran"`
	Skipped []report.Skip `json:"skipped"`
	// NotInTier are profile commands this tier does not run.
	NotInTier []string   `json:"notInTier,omitempty"`
	Mutation  []Mutation `json:"mutation,omitempty"`

	Open         []Item `json:"open"`
	Acknowledged []Item `json:"acknowledged"`
	// Fixed are findings an earlier run on this branch reported that this
	// report no longer has, although the command that found them ran.
	// NotRechecked are earlier findings whose command this report did not
	// run, so it says nothing about them.
	Fixed        []Item `json:"fixed"`
	NotRechecked []Item `json:"notRechecked,omitempty"`
	// Base is the ref the branch is measured from; BaseError says why the
	// earlier runs could not be read.
	Base      string `json:"base,omitempty"`
	BaseError string `json:"baseError,omitempty"`

	// Questions are the findings that wait for a person. The JSON's
	// questions and the Markdown's "Questions for a human" both come from
	// AllQuestions, which adds the protected changes.
	Questions []Item `json:"questions"`
	// Protected are the protected files the branch changed since Base, each
	// with the person's approval of its current content, if any. Without
	// one it is a question for a human too. ProtectedError says why they
	// could not be listed.
	Protected      []gate.ProtectedChange `json:"protected"`
	ProtectedError string                 `json:"protectedError,omitempty"`

	Verdict *report.Verdict `json:"verdict,omitempty"`
	// VerdictCurrent is whether the verdict is for this report's tree.
	VerdictCurrent bool `json:"verdictCurrent"`
}

// Ran is one command that ran.
type Ran struct {
	ID         string               `json:"id"`
	ExitCode   int                  `json:"exitCode"`
	ElapsedSec float64              `json:"elapsedSec"`
	Findings   int                  `json:"findings"`
	Mutants    *report.MutantCounts `json:"mutants,omitempty"`
	Coverage   *report.LineCounts   `json:"coverage,omitempty"`
	// Relevance is why a command with relevant_when ran.
	Relevance string `json:"relevance,omitempty"`
}

// Mutation is one mutation command's result and its survivors.
type Mutation struct {
	Command string               `json:"command"`
	Mutants *report.MutantCounts `json:"mutants,omitempty"`
	// Score is the report's score under the threshold rule, with what it
	// was held to (knowledge/decisions/mutation-threshold.md).
	Score     *report.Score `json:"score,omitempty"`
	Survivors []Item        `json:"survivors"`
}

// Item is one finding as the summary shows it.
type Item struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Command  string `json:"command"`
	Subject  string `json:"subject,omitempty"`
	Location string `json:"location,omitempty"`
	Diff     string `json:"diff,omitempty"`
	// Reason is the classification reason recorded with the kind.
	Reason string `json:"reason,omitempty"`
	AckBy  string `json:"acknowledgedBy,omitempty"`
	// SeenIn names the earlier report a fixed or not-rechecked finding is
	// from: its tier and start time.
	SeenIn string `json:"seenIn,omitempty"`
	// Ask is what a person is asked to decide, for a question.
	Ask string `json:"ask,omitempty"`
	// Glob is the protected_paths glob a protected-change question's
	// Subject matches.
	Glob string `json:"glob,omitempty"`
}

// Kinds of question that are not findings.
const (
	// QuestionProtectedChange is a protected file changed without a
	// person's approval of its content (R2).
	QuestionProtectedChange = "protected-change"
	// QuestionProtectedUnchecked says the protected paths could not be
	// listed; Reason says why.
	QuestionProtectedUnchecked = "protected-unchecked"
)

// AllQuestions is everything that waits for a person: the findings that
// need one, each protected change without an approval, and a note when the
// protected paths could not be checked. It is the one source for both the
// JSON's questions and the Markdown's section.
func (s *Summary) AllQuestions() []Item {
	qs := append([]Item{}, s.Questions...)
	for _, c := range gate.Unapproved(s.Protected) {
		qs = append(qs, Item{
			Kind:    QuestionProtectedChange,
			Subject: c.Path,
			Glob:    c.Glob,
			Ask:     fmt.Sprintf("approve this content with `reliab approve %s --by human:<name>`, or have the change reverted (R2)", c.Path),
		})
	}
	if s.ProtectedError != "" {
		qs = append(qs, Item{
			Kind:   QuestionProtectedUnchecked,
			Reason: oneLine(s.ProtectedError),
			Ask:    "run `reliab gate protected --base <ref>`",
		})
	}
	return qs
}

// MarshalJSON writes AllQuestions as questions, so the JSON and the
// Markdown hold the same questions.
func (s *Summary) MarshalJSON() ([]byte, error) {
	type plain Summary
	out := plain(*s)
	out.Questions = s.AllQuestions()
	return json.Marshal(out)
}

// asks is what a person decides for each kind that stops for one, from
// report.Kinds.
var asks = func() map[string]string {
	m := map[string]string{}
	for _, p := range report.Kinds {
		if p.Human {
			m[p.Kind] = p.Ask
		}
	}
	return m
}()

// Options select the report and the branch's base.
type Options struct {
	Root string
	Tier string
	// Base is the ref the branch forked from. Empty uses the profile's
	// base, then gitx.BaseFallbacks.
	Base string
}

// Build reads the tier's latest report and the branch's earlier reports.
func Build(opt Options) (*Summary, error) {
	r, err := report.Read(report.LatestPath(opt.Root, opt.Tier))
	if err != nil {
		return nil, fmt.Errorf("no readable %s report (run reliab verify --tier %s): %w", opt.Tier, opt.Tier, err)
	}
	s := &Summary{
		Tier: r.Run.Tier, Profile: r.Run.Profile, Tool: r.Tool.Name + " " + r.Tool.Version,
		StartedAt: r.Run.StartedAt, ElapsedSec: r.Run.ElapsedSec, BudgetSec: r.Run.BudgetSec,
		Complete: r.Run.Complete, TreeHash: r.Run.TreeHash, Head: r.Run.Head, Seed: r.Run.Seed,
		PropertyCases: r.Run.PropertyCases, Skipped: r.Run.Skipped, Verdict: r.Verdict,
		Tests: r.Run.Tests, MutationTests: r.Run.MutationTests,
		Ran: []Ran{}, Open: []Item{}, Acknowledged: []Item{}, Fixed: []Item{}, Questions: []Item{},
	}
	if s.Skipped == nil {
		s.Skipped = []report.Skip{}
	}
	s.VerdictCurrent = r.Verdict != nil && r.Verdict.TreeHash == r.Run.TreeHash

	profileBase := ""
	prof, err := profile.Load(filepath.Join(opt.Root, profile.DefaultPath))
	if err != nil {
		s.FreshError = err.Error()
	} else {
		if s.CurrentTree, err = gitx.TreeHash(opt.Root, prof.TreeExclude()); err != nil {
			s.FreshError = err.Error()
		}
		s.Fresh = err == nil && s.CurrentTree == r.Run.TreeHash
		s.NotInTier = notInTier(prof, r)
		profileBase = prof.Base
	}

	count := map[string]int{}
	for _, f := range r.Findings {
		count[f.Command]++
	}
	for _, c := range r.Run.Commands {
		s.Ran = append(s.Ran, Ran{ID: c.ID, ExitCode: c.ExitCode, ElapsedSec: c.ElapsedSec, Findings: count[c.ID], Mutants: c.Mutants, Coverage: c.Coverage, Relevance: c.Relevance})
		if profile.IsMutation(c.ID) {
			m := Mutation{Command: c.ID, Mutants: c.Mutants, Score: c.Score, Survivors: []Item{}}
			for _, f := range r.Findings {
				if f.Command == c.ID && f.Kind != report.KindBelowThreshold {
					m.Survivors = append(m.Survivors, item(f))
				}
			}
			s.Mutation = append(s.Mutation, m)
		}
	}
	for _, f := range r.Findings {
		switch f.Status {
		case report.StatusOpen:
			s.Open = append(s.Open, item(f))
		case report.StatusAcknowledged:
			s.Acknowledged = append(s.Acknowledged, item(f))
		}
		if report.NeedsHuman(f) {
			q := item(f)
			q.Ask = asks[f.Kind]
			s.Questions = append(s.Questions, q)
		}
	}
	earlier(opt, profileBase, r, s)
	protectedChanges(opt.Root, prof, s)
	return s, nil
}

// protectedChanges fills Protected from the branch's changes since the base
// earlier resolved.
func protectedChanges(root string, prof *profile.Profile, s *Summary) {
	s.Protected = []gate.ProtectedChange{}
	// if, not switch: see Markdown.
	if prof == nil {
		s.ProtectedError = "the profile could not be read"
		return
	}
	if s.Base == "" {
		s.ProtectedError = s.BaseError
		return
	}
	cs, err := gate.Protected(root, prof, s.Base)
	if err != nil {
		s.ProtectedError = err.Error()
		return
	}
	s.Protected = append(s.Protected, cs...)
}

func started(r *report.Report) time.Time {
	t, _ := time.Parse(time.RFC3339, r.Run.StartedAt)
	return t
}

func item(f report.Finding) Item {
	it := Item{ID: f.ID, Kind: f.Kind, Status: f.Status, Command: f.Command, Subject: f.Test, Location: f.Location, Diff: f.Diff}
	if f.Classification != nil {
		it.Reason = f.Classification.Reason
	}
	if f.Acknowledged != nil {
		it.AckBy = f.Acknowledged.By
	}
	return it
}

// notInTier lists the profile's commands that the report's tier does not
// run. An on-demand report ran what it was asked to, so it has none.
func notInTier(p *profile.Profile, r *report.Report) []string {
	tier, ok := verify.ResolveTier(p, r.Run.Tier)
	if !ok {
		return nil
	}
	var out []string
	for id := range p.Commands {
		if !slices.Contains(tier.Run, id) && id != verify.CharacterizeID {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// earlier fills Fixed and NotRechecked from the history reports of this
// branch: runs whose HEAD lies between the merge base with the base ref and
// the current HEAD, started before this report. For each finding id the
// latest such run wins.
func earlier(opt Options, profileBase string, r *report.Report, s *Summary) {
	base, err := gitx.ResolveBase(opt.Root, opt.Base, profileBase)
	if err != nil {
		s.BaseError = err.Error()
		return
	}
	s.Base = base
	mb, err := gitx.MergeBase(opt.Root, base)
	if err != nil {
		s.BaseError = fmt.Sprintf("no merge base with %s: %v", base, err)
		return
	}
	head := gitx.Head(opt.Root)
	onBranch := map[string]bool{}
	inBranch := func(h string) bool {
		if h == "" {
			return false
		}
		if v, ok := onBranch[h]; ok {
			return v
		}
		v := gitx.IsAncestor(opt.Root, mb, h) && gitx.IsAncestor(opt.Root, h, head)
		onBranch[h] = v
		return v
	}
	now := map[string]bool{}
	for _, f := range r.Findings {
		now[f.ID] = true
	}
	ran := map[string]bool{}
	for _, c := range r.Run.Commands {
		ran[c.ID] = true
	}
	last := map[string]Item{}
	var order []string
	for _, h := range report.History(opt.Root) {
		// Later runs do not count. This report is in the history too, but
		// its findings are all current, so it adds nothing.
		if started(h).After(started(r)) {
			continue
		}
		if !inBranch(h.Run.Head) {
			continue
		}
		for _, f := range h.Findings {
			if now[f.ID] {
				continue
			}
			it := item(f)
			it.SeenIn = h.Run.Tier + " " + h.Run.StartedAt
			if _, seen := last[f.ID]; !seen {
				order = append(order, f.ID)
			}
			last[f.ID] = it
		}
	}
	for _, id := range order {
		it := last[id]
		if ran[it.Command] {
			s.Fixed = append(s.Fixed, it)
		} else {
			s.NotRechecked = append(s.NotRechecked, it)
		}
	}
}

// Write renders s in format to w.
func Write(w io.Writer, s *Summary, format string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	case "markdown":
		_, err := io.WriteString(w, Markdown(s))
		return err
	}
	return fmt.Errorf("format %q: want one of %s", format, strings.Join(Formats, ", "))
}

// Markdown renders s as a PR description's evidence section.
func Markdown(s *Summary) string {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	p("### Evidence: reliab %s report\n\n", s.Tier)
	// if, not switch: Go coverage does not instrument case expressions, so
	// gremlins would call their mutants not covered.
	if s.FreshError != "" {
		p("> **Freshness unknown:** %s\n\n", oneLine(s.FreshError))
	} else if !s.Fresh {
		p("> **Stale:** the working tree changed after this report ran. It describes tree `%s`; the tree is now `%s`. Re-run `reliab verify --tier %s` before relying on it.\n\n",
			short(s.TreeHash), short(s.CurrentTree), s.Tier)
	}
	if !s.Complete {
		p("> **Incomplete:** %s; see Skipped.\n\n", incomplete(s))
	}

	complete := "complete"
	if !s.Complete {
		complete = "incomplete"
	}
	p("- Run: %s tier, profile `%s`, %s, started %s, %.1fs of a %s budget, %s.\n",
		s.Tier, s.Profile, s.Tool, s.StartedAt, s.ElapsedSec, budget(s.BudgetSec), complete)
	at := ""
	if s.Head != "" {
		at = fmt.Sprintf(" at commit `%s`", short(s.Head))
	}
	p("- Tree `%s`%s", short(s.TreeHash), at)
	if s.Seed != "" {
		p("; seed %s", s.Seed)
	}
	if s.PropertyCases > 0 {
		p(", %d property cases", s.PropertyCases)
	}
	p(".\n")
	if len(s.Tests) > 0 {
		p("- Tests named (`--tests`): %s.\n", codeList(s.Tests))
	}
	if len(s.MutationTests) > 0 {
		p("- Mutation tests named (`--mutation-tests`): %s.\n", codeList(s.MutationTests))
	}
	if v := s.Verdict; v == nil {
		p("- Reviewer verdict: none recorded.\n")
	} else {
		bound := "for this report's tree"
		if !s.VerdictCurrent {
			bound = "**not for this report's tree**"
		}
		p("- Reviewer verdict: **%s** by %s at %s, %s.", v.Outcome, v.By, v.At, bound)
		if v.Notes != "" {
			p(" Notes: %s", oneLine(v.Notes))
		}
		p("\n")
	}

	p("\n#### Ran\n\n| Command | Exit | Findings | Time |\n|---|---|---|---|\n")
	for _, c := range s.Ran {
		p("| %s | %d | %d | %.1fs |\n", c.ID, c.ExitCode, c.Findings, c.ElapsedSec)
	}
	if len(s.Ran) == 0 {
		p("| (none) | | | |\n")
	}
	var relevant []Ran
	for _, c := range s.Ran {
		if c.Relevance != "" {
			relevant = append(relevant, c)
		}
	}
	if len(relevant) > 0 {
		p("\nWhy commands with relevance rules ran:\n\n")
		for _, c := range relevant {
			p("- `%s`: %s.\n", c.ID, oneLine(c.Relevance))
		}
	}

	if len(s.Skipped) > 0 || len(s.NotInTier) > 0 {
		p("\n#### Skipped\n\n")
		if len(s.Skipped) > 0 {
			p("| Command | Why | Detail |\n|---|---|---|\n")
			for _, k := range s.Skipped {
				p("| %s | %s | %s |\n", k.ID, skipWhy(k.Reason), cell(k.Detail))
			}
		}
		if len(s.NotInTier) > 0 {
			if len(s.Skipped) > 0 {
				p("\n")
			}
			p("Not in the %s tier: %s.\n", s.Tier, strings.Join(s.NotInTier, ", "))
		}
	}

	var covered []Ran
	for _, c := range s.Ran {
		if c.Coverage != nil {
			covered = append(covered, c)
		}
	}
	if len(covered) > 0 {
		p("\n#### Coverage\n\n")
		for _, c := range covered {
			v := c.Coverage
			pct := 0.0
			if v.Valid > 0 {
				pct = 100 * float64(v.Covered) / float64(v.Valid)
			}
			p("- `%s`: %d of %d lines covered (%.1f%%)", c.ID, v.Covered, v.Valid, pct)
			if v.ChangedSince != "" {
				p(", lines changed since `%s` only", v.ChangedSince)
			}
			p(".\n")
		}
	}

	if len(s.Mutation) > 0 {
		p("\n#### Mutation\n\n")
		for _, m := range s.Mutation {
			if c := m.Mutants; c != nil {
				p("- `%s`: %d tested, %d killed, %d survived", m.Command, c.Killed+c.Survived, c.Killed, c.Survived)
				if c.NotCovered > 0 {
					p("; %d not covered by any test", c.NotCovered)
				}
				if c.ChangedSince != "" {
					p(" (lines changed since `%s` only)", c.ChangedSince)
				}
				p(".\n")
			} else {
				p("- `%s`: the tool reported no counts; %d survivor finding(s).\n", m.Command, len(m.Survivors))
			}
			if sc := m.Score; sc != nil {
				p("  - Score %s", score(sc.Killed, sc.Counted))
				if sc.Equivalent > 0 {
					p(", less %d survivor(s) a person confirmed equivalent", sc.Equivalent)
				}
				if sc.PendingEquivalent > 0 {
					p("; %d classified equivalent and waiting for a person, still counted", sc.PendingEquivalent)
				}
				p(".")
				if sc.Threshold != nil {
					p(" Threshold %s%%: %s.", num(*sc.Threshold), met(sc.Killed, sc.Counted, *sc.Threshold))
				}
				p("\n")
				for _, b := range sc.Baselines {
					if b.Unmeasured {
						p("  - Baseline `%s` %s%%: not scored, the tool reported no per-file counts.\n", b.Scope, num(b.Baseline))
						continue
					}
					p("  - Baseline `%s` %s%%: %s here, %s.\n", b.Scope, num(b.Baseline), score(b.Killed, b.Counted), met(b.Killed, b.Counted, b.Baseline))
				}
			}
		}
		var all []Item
		for _, m := range s.Mutation {
			all = append(all, m.Survivors...)
		}
		if len(all) > 0 {
			p("\n| Survivor | At | Kind | Status | Reason |\n|---|---|---|---|---|\n")
			for _, it := range all {
				p("| `%s` %s | %s | %s | %s | %s |\n", it.ID, cell(it.Subject), cell(it.Location), it.Kind, status(it), cell(it.Reason))
			}
		}
	}

	p("\n#### Findings\n\n")
	p("- Open: %d. Acknowledged by a person: %d. Fixed during the change: %d", len(s.Open), len(s.Acknowledged), len(s.Fixed))
	if s.BaseError != "" {
		p(" (earlier runs not read: %s)", oneLine(s.BaseError))
	}
	p(".\n")
	if s.Base != "" {
		p("- Fixed means reported by an earlier run on this branch (since its merge base with `%s`) and not by this one, although the command that found it ran.\n", s.Base)
	}
	table := func(title string, items []Item, seen bool) {
		if len(items) == 0 {
			return
		}
		p("\n%s\n\n", title)
		if seen {
			p("| Finding | Kind | Subject | Last seen | Reason |\n|---|---|---|---|---|\n")
			for _, it := range items {
				p("| `%s` | %s | %s | %s | %s |\n", it.ID, it.Kind, subject(it), it.SeenIn, cell(it.Reason))
			}
			return
		}
		p("| Finding | Kind | Subject | Reason |\n|---|---|---|---|\n")
		for _, it := range items {
			p("| `%s` | %s | %s | %s |\n", it.ID, it.Kind, subject(it), cell(reasonAck(it)))
		}
	}
	table("Open:", s.Open, false)
	table("Acknowledged:", s.Acknowledged, false)
	table("Fixed during the change:", s.Fixed, true)
	table(fmt.Sprintf("Reported earlier by commands this report did not run (%d), so not re-checked:", len(s.NotRechecked)), s.NotRechecked, true)

	p("\n#### Questions for a human\n\n")
	qs := s.AllQuestions()
	if len(qs) == 0 {
		p("None: no open finding or protected-path change waits for a person.\n")
	}
	n := 0
	for _, q := range qs {
		// if, not switch: see above.
		if q.Kind == QuestionProtectedUnchecked {
			p("- Protected paths not checked: %s. Run `reliab gate protected --base <ref>`.\n", q.Reason)
			continue
		}
		n++
		if q.Kind == QuestionProtectedChange {
			p("%d. `%s` (protected by `%s`) changed since `%s` without a person's approval: %s\n", n, q.Subject, q.Glob, s.Base, q.Ask)
			continue
		}
		p("%d. `%s` [%s] %s: %s", n, q.ID, q.Kind, subject(q), q.Ask)
		if q.Reason != "" {
			p(". Recorded reason: %q", oneLine(q.Reason))
		}
		p("\n")
	}
	if len(gate.Unapproved(s.Protected)) < len(s.Protected) {
		p("\nProtected changes a person approved (a local record; CI and the code host's review still decide):\n\n")
		for _, c := range s.Protected {
			if a := c.Approval; a != nil {
				p("- `%s` (protected by `%s`): approved by %s at %s.\n", c.Path, c.Glob, a.By, a.At)
			}
		}
	}
	return b.String()
}

func subject(it Item) string {
	s := it.Subject
	if s == "" {
		s = "command " + it.Command
	}
	if it.Location != "" && !strings.Contains(s, it.Location) {
		s += " at " + it.Location
	}
	if it.Diff != "" {
		s += " (diff: " + it.Diff + ")"
	}
	return cell(s)
}

func status(it Item) string {
	if it.AckBy != "" {
		return "acknowledged by " + it.AckBy
	}
	return it.Status
}

func reasonAck(it Item) string {
	if it.AckBy != "" {
		return it.Reason + " (acknowledged by " + it.AckBy + ")"
	}
	return it.Reason
}

var skipWhys = map[string]string{
	report.SkipBudget:        "budget ran out",
	report.SkipNoCommand:     "no command in the profile",
	report.SkipNotApplicable: "role not applicable",
	report.SkipBuildFailed:   "the build failed",
	report.SkipNotRelevant:   "not relevant to the change",
	report.SkipRoleMissing:   "role needed but missing",
	report.SkipNeedsInput:    "no tests named (incomplete)",
}

// incomplete says why the run is incomplete: tests not named, the budget,
// or both.
func incomplete(s *Summary) string {
	var unnamed []string
	budgetOut := false
	for _, k := range s.Skipped {
		if k.Reason == report.SkipNeedsInput {
			unnamed = append(unnamed, k.ID)
		}
		if k.Reason == report.SkipBudget {
			budgetOut = true
		}
	}
	var why []string
	if len(unnamed) > 0 {
		why = append(why, "no tests were named for "+strings.Join(unnamed, ", ")+", which need them")
	}
	if budgetOut || len(unnamed) == 0 {
		why = append(why, fmt.Sprintf("the %s budget ran out before every command ran", budget(s.BudgetSec)))
	}
	return strings.Join(why, ", and ")
}

// codeList renders items as inline code, separated by semicolons, since a
// test spec may itself contain commas.
func codeList(items []string) string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = "`" + strings.ReplaceAll(oneLine(it), "`", "'") + "`"
	}
	return strings.Join(out, "; ")
}

func skipWhy(reason string) string {
	if w, ok := skipWhys[reason]; ok {
		return w
	}
	return reason
}

// score renders killed of counted under the threshold rule.
func score(killed, counted int) string {
	if counted == 0 {
		return "not defined: no mutants counted"
	}
	return fmt.Sprintf("%.1f%% (%d killed of %d counted)", report.Percent(killed, counted), killed, counted)
}

func met(killed, counted int, want float64) string {
	if counted == 0 {
		return "nothing to hold"
	}
	if report.Below(killed, counted, want) {
		return "**below**"
	}
	return "met"
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func budget(sec int) string {
	if sec == 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%ds", sec)
}

func short(h string) string { return h[:min(len(h), 12)] }

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// cell makes text safe inside a Markdown table cell.
func cell(s string) string { return strings.ReplaceAll(oneLine(s), "|", `\|`) }
