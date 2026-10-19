// Package triage acts on findings in the latest reports: classifying them,
// recording human acknowledgements, and saying what to do next per kind.
package triage

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/hbaldwin98/reliab/internal/mutscore"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/pkg/snapdiff"
)

// Tiers whose latest reports triage reads and updates. Nightly findings go
// through the deferred findings queue.
var Tiers = []string{"inner", "prepr", report.OnDemand}

// ClassifiableKinds are the kinds an agent may assign. It may route a
// finding to a human kind; it may not acknowledge it.
var ClassifiableKinds = []string{
	report.KindNewCodeDefect, report.KindHarnessDefect, report.KindWrongProperty,
	report.KindLegacyDefectOrQuirk, report.KindCharacterizationDiff, report.KindEquivalentMutant,
	report.KindRequirementSilent, report.KindEnvironment,
}

// Classify sets the kind of finding id in every latest report that holds
// it, and returns the tiers it updated; a *NotRescored error comes with
// them when the scores could not be recomputed. It clears a reviewer verdict on a
// report it changes: the reviewer judged the classifications as they were
// (knowledge/decisions/reviewer-verdict.md).
//
// A finding no latest report holds was fixed before it was classified. R4
// still wants its kind on record, so Classify then updates the newest
// history report that holds it, the run the PR summary's fixed list reads,
// and clears the verdict on the latest pre-PR report, which the summary
// renders that list beside.
func Classify(root, id, kind, reason string, now time.Time) ([]string, error) {
	if !contains(ClassifiableKinds, kind) {
		return nil, fmt.Errorf("kind %q: want one of %s", kind, strings.Join(ClassifiableKinds, ", "))
	}
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("a one-line --reason is required (R4)")
	}
	change := func(f *report.Finding) error {
		if f.Status == report.StatusAcknowledged {
			return fmt.Errorf("%s was acknowledged by %s; it cannot be reclassified", f.ID, f.Acknowledged.By)
		}
		if f.Kind == report.KindBelowThreshold {
			return fmt.Errorf("%s is a mutation score below what it must reach; it is derived from its survivors and is never classified: handle each survivor per reliab report route", f.ID)
		}
		if kind == report.KindEquivalentMutant && !isMutation(*f) {
			return fmt.Errorf("%s comes from %s, not a mutation command; only a surviving mutant can be %s", f.ID, f.Command, kind)
		}
		if kind == report.KindRequirementSilent && !isMutation(*f) && !isGenerated(*f) {
			return fmt.Errorf("%s comes from %s: not a mutation command, nor a property or fuzz command; only their findings can be %s", f.ID, f.Command, kind)
		}
		f.Kind = kind
		f.Classification = &report.Classification{Reason: reason, At: report.Now(now)}
		return nil
	}
	updated, err := update(root, id, true, change)
	if err == nil || len(updated) > 0 {
		return updated, err
	}
	return classifyFixed(root, id, change, fmt.Errorf("%w, nor in history", err))
}

// classifyFixed applies change to the newest history report that holds id,
// or returns notFound when none does.
func classifyFixed(root, id string, change func(*report.Finding) error, notFound error) ([]string, error) {
	hist := report.History(root)
	for i := len(hist) - 1; i >= 0; i-- {
		r := hist[i]
		hit := false
		for j := range r.Findings {
			if r.Findings[j].ID != id {
				continue
			}
			if err := change(&r.Findings[j]); err != nil {
				return nil, err
			}
			hit = true
		}
		if !hit {
			continue
		}
		var stale error
		if err := rescore(root, r); err != nil {
			stale = &NotRescored{Reasons: []string{"history: " + err.Error()}}
		}
		if err := report.RewriteHistory(root, r); err != nil {
			return nil, err
		}
		if latest, err := report.Read(report.LatestPath(root, "prepr")); err == nil && latest.Verdict != nil {
			latest.Verdict = nil
			if err := report.Rewrite(root, latest); err != nil {
				return nil, err
			}
		}
		return []string{fmt.Sprintf("history %s %s", r.Run.Tier, r.Run.StartedAt)}, stale
	}
	return nil, notFound
}

// Acknowledge records that a person has decided a human-kind finding. Hooks
// deny this command to agents.
func Acknowledge(root, id, by string, now time.Time) ([]string, error) {
	if strings.TrimSpace(by) == "" {
		return nil, fmt.Errorf("--by is required, e.g. human:<name>")
	}
	return update(root, id, false, func(f *report.Finding) error {
		if !contains(report.HumanKinds, f.Kind) {
			return fmt.Errorf("%s is %s; only %s findings are acknowledged", f.ID, f.Kind, strings.Join(report.HumanKinds, " or "))
		}
		f.Acknowledged = &report.Acknowledged{By: by, At: report.Now(now)}
		f.Status = report.StatusAcknowledged
		return nil
	})
}

func update(root, id string, clearVerdict bool, change func(*report.Finding) error) ([]string, error) {
	var updated, notRescored []string
	for _, tier := range Tiers {
		r, err := report.Read(report.LatestPath(root, tier))
		if err != nil {
			continue
		}
		hit := false
		for i := range r.Findings {
			if r.Findings[i].ID != id {
				continue
			}
			if err := change(&r.Findings[i]); err != nil {
				return updated, err
			}
			hit = true
		}
		if !hit {
			continue
		}
		// A survivor a person confirms equivalent leaves the score's count,
		// so the score and its shortfall are recomputed.
		if err := rescore(root, r); err != nil {
			notRescored = append(notRescored, tier+": "+err.Error())
		}
		if clearVerdict {
			r.Verdict = nil
		}
		if err := report.Rewrite(root, r); err != nil {
			return updated, err
		}
		updated = append(updated, tier)
	}
	if len(updated) == 0 {
		return nil, fmt.Errorf("no finding %s in the latest %s reports", id, strings.Join(Tiers, " or "))
	}
	if len(notRescored) > 0 {
		return updated, &NotRescored{Reasons: notRescored}
	}
	return updated, nil
}

// NotRescored is returned, with the tiers updated, when a change was
// recorded but a report's mutation scores could not be recomputed, so its
// scores and shortfalls are as they were
// (knowledge/decisions/mutation-threshold.md).
type NotRescored struct{ Reasons []string }

func (e *NotRescored) Error() string {
	return "recorded, but the mutation scores were not recomputed, so they and any below-threshold finding may be out of date (" +
		strings.Join(e.Reasons, "; ") + "); fix it and re-run reliab verify"
}

// rescore recomputes a report's mutation scores and shortfalls. A report
// with no mutation counts has nothing to recompute.
func rescore(root string, r *report.Report) error {
	scored := false
	for _, c := range r.Run.Commands {
		if mutscore.IsMutation(c.ID) && c.Mutants != nil {
			scored = true
		}
	}
	if !scored {
		return nil
	}
	p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
	if err != nil {
		return err
	}
	b, err := mutscore.LoadBaseline(root, p)
	if err != nil {
		return err
	}
	mutscore.Apply(r, p, b)
	return nil
}

// actions is the triage table (paper Figure 11, AGENTS.md template).
var actions = map[string]string{
	report.KindUnclassified:         "reproduce from its seed or input, shrink it, then: reliab findings classify %s --kind <kind> --reason \"...\"",
	report.KindNewCodeDefect:        "fix product code, never the test (R1); re-run reliab verify",
	report.KindHarnessDefect:        "fix the test harness; re-run reliab verify",
	report.KindWrongProperty:        "delete the property and write down why; re-run reliab verify",
	report.KindLegacyDefectOrQuirk:  "STOP: a human decides (R9); do not fix it",
	report.KindCharacterizationDiff: "STOP: a human approves or reverts the snapshot (R2)",
	report.KindEquivalentMutant:     "STOP: a human confirms it is equivalent, then acknowledges it or excludes it in the mutation tool's config (R7)",
	report.KindRequirementSilent:    "STOP: a human says whether this behavior is intended; then a test that cites the answer pins it, or the code changes (R7)",
	report.KindEnvironment:          "STOP: the machine failed, not the code; a human fixes the environment, then re-run (this classification does not carry to the next run)",
	report.KindBelowThreshold:       "STOP: every survivor counted against this score is with a person or decided; a person accepts this score for the change (reliab findings ack) or says what should be tested. Never write a test only to raise the score (R7)",
}

// shortfallAction replaces the below-threshold action while survivors it
// counts still need the agent: the score resolves with them
// (knowledge/decisions/mutation-threshold.md).
const shortfallAction = "the score is short because of its survivors, which are findings of their own: handle those first, each per its own route (%d still need you: %s). This finding adds no work of its own and changes as they do; never write a test only to raise the score (R7)"

// mutantAction replaces the unclassified action for a surviving mutant,
// which has no seed to reproduce (R7). Its message is a diff, shown in full
// up to mutantLines.
const (
	// One literal: gremlins mutates the + of a concatenated constant, which
	// cannot compile, and reports it uncovered because constants have no
	// coverage blocks.
	mutantAction = "find where the mutated behavior is intended (a ticket, a doc, a rule, a caller that depends on it). Stated: write a test that cites it and fails with this mutation. Nothing states it: do not pin it; reliab findings classify %[1]s --kind requirement-silent --reason \"<the question>\". No test can kill it: reliab findings classify %[1]s --kind equivalent-mutant --reason \"...\""
	mutantLines  = 24
)

// commandAction replaces the unclassified action for a command that failed
// without reporting a test: a build error, a crashed tool, or a missing
// result file. There is no seed to reproduce and, for a mutation command,
// no mutant to kill.
// generatedAction replaces the unclassified action for a property or fuzz
// failure, whose input was generated: the cited source may say nothing about
// it, and behavior new on the branch is not a legacy quirk.
const generatedAction = "reproduce it from its seed or saved input and shrink it, then compare it with the source the property cites. The code breaks what the source states: fix it (new-code-defect), or, if the behavior predates the change, reliab findings classify %[1]s --kind legacy-defect-or-quirk. The property or generator is wrong: harness-defect or wrong-property. The source says nothing about this input (no limit stated, say): do not pick a side; reliab findings classify %[1]s --kind requirement-silent --reason \"<the question>\""

const commandAction = "the command failed before reporting a result; read the message and its log. A build or test-code error: fix it and re-run (new-code-defect or harness-defect). The tool or machine failed: re-run once, then reliab findings classify %s --kind environment --reason \"<the machine's error>\""

// snapshotAction replaces the unclassified action for a text snapshot or
// capture that no longer matches, which carries a diff label
// (knowledge/decisions/snapshot-diff-labels.md). Its message is the labelled
// diff, shown up to snapshotLines.
const (
	snapshotAction = "a pinned output changed. Your change meant to change it: reliab findings classify %[1]s --kind characterization-diff --reason \"<what changed it and why>\", then stop; a person approves or reverts it (R2). It did not: a side effect of your change; reliab findings classify %[1]s --kind new-code-defect --reason \"...\" and fix product code, never the snapshot"
	snapshotLines  = 24
)

func isMutation(f report.Finding) bool { return strings.HasPrefix(f.Command, "mutation_") }

// generatedCommands run property and fuzz tests: their failing inputs are
// generated, so the source a property cites can be silent on one.
var generatedCommands = []string{"properties", "model_tests", "api_fuzz"}

func isGenerated(f report.Finding) bool { return contains(generatedCommands, f.Command) }

// Route writes what to do next for each open finding in the most recent
// latest report, and returns how many open findings it listed.
func Route(w io.Writer, root string) (int, error) {
	r := report.Latest(root, Tiers...)
	if r == nil {
		fmt.Fprintln(w, "no report yet: run reliab verify --tier inner")
		return 0, nil
	}
	fmt.Fprintf(w, "%s report from %s (complete=%t)\n", r.Run.Tier, r.Run.StartedAt, r.Run.Complete)
	open := 0
	for _, f := range r.Findings {
		if f.Status != report.StatusOpen {
			continue
		}
		open++
		subject := f.Test
		if subject == "" {
			subject = "command " + f.Command
		}
		action := actions[f.Kind]
		if f.Kind == report.KindUnclassified && isMutation(f) {
			action = mutantAction
		}
		if f.Kind == report.KindUnclassified && isGenerated(f) {
			action = generatedAction
		}
		if f.Kind == report.KindUnclassified && f.Test == "" {
			action = commandAction
			if log := commandLog(r, f.Command); log != "" {
				action += "; its log is " + log
			}
		}
		if f.Kind == report.KindUnclassified && f.Diff != "" {
			action = snapshotAction
		}
		if strings.Contains(action, "%") {
			action = fmt.Sprintf(action, f.ID)
		}
		if f.Kind == report.KindBelowThreshold {
			if owed := owedSurvivors(r, f); len(owed) > 0 {
				action = fmt.Sprintf(shortfallAction, len(owed), strings.Join(owed, ", "))
			}
		}
		fmt.Fprintf(w, "\n%s  [%s]  %s\n  -> %s\n", f.ID, f.Kind, subject, action)
		if f.Reopened != "" {
			fmt.Fprintf(w, "  reopened: %s\n", f.Reopened)
		}
		if f.Location != "" {
			fmt.Fprintf(w, "  at %s\n", f.Location)
		}
		if f.Diff != "" {
			fmt.Fprintf(w, "  diff: %s\n", snapdiff.Describe(f.Diff))
		}
		if f.Message != "" {
			n := 4
			if isMutation(f) {
				n = mutantLines
			}
			if f.Diff != "" {
				n = snapshotLines
			}
			fmt.Fprintf(w, "  %s\n", firstLines(f.Message, n))
		}
	}
	if open == 0 {
		fmt.Fprintln(w, "no open findings")
	}
	for _, s := range r.Run.Skipped {
		if s.Reason == report.SkipBudget {
			fmt.Fprintf(w, "skipped %s: budget; report it in the PR summary (R8)\n", s.ID)
		}
		if s.Reason == report.SkipBuildFailed {
			fmt.Fprintf(w, "skipped %s: the build failed; fix the build and re-run\n", s.ID)
		}
		if s.Reason == report.SkipNotRelevant {
			fmt.Fprintf(w, "skipped %s: not relevant: %s; report it in the PR summary (R8)\n", s.ID, s.Detail)
		}
	}
	for _, c := range r.Run.Commands {
		if c.Relevance != "" {
			fmt.Fprintf(w, "ran %s: %s\n", c.ID, c.Relevance)
		}
	}
	return open, nil
}

// owedSurvivors are the survivors a shortfall counts that still need the
// agent: for a baseline scope, only those in the scope. A shortfall no
// longer matching the report's scores falls back to the command's.
func owedSurvivors(r *report.Report, short report.Finding) []string {
	ch, ok := mutscore.CheckOf(r, short)
	if !ok {
		ch = mutscore.Check{Command: short.Command}
	}
	var out []string
	for _, f := range mutscore.Survivors(ch, r.Findings) {
		if report.NeedsAgent(f) {
			out = append(out, f.ID)
		}
	}
	return out
}

// firstLines keeps the first n lines of s and, when it cuts any, the last
// tailLines too: tools often end a failure with how to reproduce it.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n+tailLines+1 {
		lines = append(append(lines[:n:n], "..."), lines[len(lines)-tailLines:]...)
	}
	return strings.Join(lines, "\n  ")
}

// tailLines is how much of a cut message's end route keeps.
const tailLines = 3

// commandLog is the log of the command that produced a finding.
func commandLog(r *report.Report, id string) string {
	for _, c := range r.Run.Commands {
		if c.ID == id {
			return c.Log
		}
	}
	return ""
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
