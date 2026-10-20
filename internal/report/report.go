// Package report defines the report document (schemas/report.schema.json)
// and reads and writes it under .reliab/reports/.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/hbaldwin98/reliab/schemas"
)

// SchemaVersion is the report schema version this build writes.
const SchemaVersion = "0.10.0"

// Dir is where reports live, relative to the repo root. Only reliab writes
// here; see knowledge/decisions/report-storage.md.
const Dir = ".reliab/reports"

// OnDemand is the tier recorded for a run whose commands were chosen with
// verify --only. Its report never stands in for a tier's in the hooks.
const OnDemand = "on-demand"

// Finding kinds. See knowledge/concepts/finding-kind.md.
const (
	KindUnclassified         = "unclassified"
	KindNewCodeDefect        = "new-code-defect"
	KindHarnessDefect        = "harness-defect"
	KindWrongProperty        = "wrong-property"
	KindLegacyDefectOrQuirk  = "legacy-defect-or-quirk"
	KindCharacterizationDiff = "characterization-diff"
	KindEquivalentMutant     = "equivalent-mutant"
	KindRequirementSilent    = "requirement-silent"
	KindEnvironment          = "environment"
	// KindBelowThreshold is a mutation command's score below the profile's
	// threshold or a scope's baseline. verify derives it from the counts and
	// the survivors each run; it is never classified, only acknowledged
	// (knowledge/decisions/mutation-threshold.md).
	KindBelowThreshold = "below-threshold"
)

// Finding statuses.
const (
	StatusOpen         = "open"
	StatusFixed        = "fixed"
	StatusDiscarded    = "discarded"
	StatusAcknowledged = "acknowledged"
)

type Report struct {
	SchemaVersion string    `json:"schemaVersion"`
	Tool          Tool      `json:"tool"`
	Run           Run       `json:"run"`
	Findings      []Finding `json:"findings"`
	Verdict       *Verdict  `json:"verdict,omitempty"`
}

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Run struct {
	Tier          string          `json:"tier"`
	Only          []string        `json:"only,omitempty"`
	Scope         []string        `json:"scope,omitempty"`
	Profile       string          `json:"profile"`
	StartedAt     string          `json:"startedAt"`
	ElapsedSec    float64         `json:"elapsedSec"`
	BudgetSec     int             `json:"budgetSec"`
	Complete      bool            `json:"complete"`
	TreeHash      string          `json:"treeHash"`
	Head          string          `json:"head,omitempty"`
	Seed          string          `json:"seed,omitempty"`
	Base          string          `json:"base,omitempty"`
	PropertyCases int             `json:"propertyCases,omitempty"`
	Commands      []CommandResult `json:"commands"`
	Skipped       []Skip          `json:"skipped"`
}

type CommandResult struct {
	ID         string  `json:"id"`
	ExitCode   int     `json:"exitCode"`
	ElapsedSec float64 `json:"elapsedSec"`
	Format     string  `json:"format,omitempty"`
	Output     string  `json:"output,omitempty"`
	Log        string  `json:"log,omitempty"`
	// Mutants counts what a mutation command tested, when its format says.
	Mutants *MutantCounts `json:"mutants,omitempty"`
	// MutantFiles breaks Mutants down by repo-relative path, when the tool
	// says, so a baseline scope can be scored.
	MutantFiles map[string]FileCounts `json:"mutantFiles,omitempty"`
	// Coverage counts the lines a coverage report measured, when its
	// format says.
	Coverage *LineCounts `json:"coverage,omitempty"`
	// Score is a mutation command's score under the threshold rule, with the
	// profile's threshold and the baseline scopes it was held to.
	Score *Score `json:"score,omitempty"`
	// Relevance says why a command with relevant_when ran: the changed file
	// that matched, or why relevance was not judged.
	Relevance string `json:"relevance,omitempty"`
}

// Score is a mutation score: Killed of Counted, where Counted is killed,
// survived, and not covered, less the mutants a person confirmed equivalent
// (knowledge/decisions/mutation-threshold.md).
type Score struct {
	Killed  int `json:"killed"`
	Counted int `json:"counted"`
	// Equivalent is how many survivors a person confirmed equivalent; they
	// are left out of Counted. PendingEquivalent are classified equivalent
	// but not yet confirmed, and still counted.
	Equivalent        int `json:"equivalent,omitempty"`
	PendingEquivalent int `json:"pendingEquivalent,omitempty"`
	// Threshold is the profile's minimum score in percent, when it sets one.
	Threshold *float64     `json:"threshold,omitempty"`
	Baselines []ScopeScore `json:"baselines,omitempty"`
}

// ScopeScore is the score of the mutants in one baseline scope.
type ScopeScore struct {
	Scope    string  `json:"scope"`
	Baseline float64 `json:"baseline"`
	Killed   int     `json:"killed"`
	Counted  int     `json:"counted"`
	// Unmeasured: the tool reported no per-file counts, so the scope's score
	// is unknown.
	Unmeasured bool `json:"unmeasured,omitempty"`
}

// Percent is killed over counted, in percent; 0 when nothing was counted.
func Percent(killed, counted int) float64 {
	if counted == 0 {
		return 0
	}
	return 100 * float64(killed) / float64(counted)
}

// Below reports whether killed of counted falls short of want percent.
// Nothing counted is never below: there is nothing to hold.
func Below(killed, counted int, want float64) bool {
	return counted > 0 && 100*float64(killed) < want*float64(counted)
}

// LineCounts is how many lines a coverage report measured and how many
// tests reached. ChangedSince is set when they count only lines changed
// since that base ref.
type LineCounts struct {
	Valid        int    `json:"valid"`
	Covered      int    `json:"covered"`
	ChangedSince string `json:"changedSince,omitempty"`
}

// MutantCounts is how many mutants a mutation command killed (timeouts
// included), how many survived, and how many no test covers.
type MutantCounts struct {
	Killed     int `json:"killed"`
	Survived   int `json:"survived"`
	NotCovered int `json:"notCovered,omitempty"`
	// ChangedSince is set when the counts cover only mutants on lines
	// changed since that base ref.
	ChangedSince string `json:"changedSince,omitempty"`
}

// FileCounts are one file's mutant counts.
type FileCounts struct {
	Killed     int `json:"killed"`
	Survived   int `json:"survived"`
	NotCovered int `json:"notCovered,omitempty"`
}

// Skip reasons.
const (
	SkipBudget        = "budget"
	SkipNoCommand     = "no-command"
	SkipNotApplicable = "not-applicable"
	// SkipBuildFailed: the build command failed, so a later command would
	// test stale binaries.
	SkipBuildFailed = "build-failed"
	// SkipNotRelevant: no file the change touched matches the command's
	// relevant_when globs.
	SkipNotRelevant = "not-relevant"
	// SkipRoleMissing: the codebase needs the command's tool role but has
	// no tool for it yet.
	SkipRoleMissing = "role-missing"
)

type Skip struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

type Finding struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Command        string          `json:"command"`
	Test           string          `json:"test,omitempty"`
	Location       string          `json:"location,omitempty"`
	Intent         string          `json:"intent,omitempty"`
	Replay         string          `json:"replay,omitempty"`
	MinimalInput   any             `json:"minimalInput,omitempty"`
	Observed       string          `json:"observed,omitempty"`
	Expected       string          `json:"expected,omitempty"`
	Message        string          `json:"message,omitempty"`
	Diff           string          `json:"diff,omitempty"`
	Status         string          `json:"status"`
	Score          *int            `json:"score,omitempty"`
	Classification *Classification `json:"classification,omitempty"`
	Acknowledged   *Acknowledged   `json:"acknowledged,omitempty"`
	// Fingerprint identifies a command-level finding's failure (one with no
	// Test): a hash of its message and observed output without noise. Its
	// classification and acknowledgement carry to the next run only while
	// the fingerprint is the same (knowledge/decisions/finding-classification.md).
	Fingerprint string `json:"fingerprint,omitempty"`
	// Reopened says why an earlier classification or acknowledgement of this
	// finding was not carried: the failure changed, or the earlier report
	// had no fingerprint.
	Reopened string `json:"reopened,omitempty"`
}

type Classification struct {
	Reason string `json:"reason"`
	At     string `json:"at"`
	// From names the report a carried classification was made in, as its
	// tier and run start; empty when it was made on this report.
	From string `json:"from,omitempty"`
}

type Acknowledged struct {
	By string `json:"by"`
	At string `json:"at"`
	// From names the report a carried acknowledgement was made in, as for
	// Classification.
	From string `json:"from,omitempty"`
}

// Verdict outcomes.
const (
	VerdictPass = "pass"
	VerdictFail = "fail"
)

// Verdict is the reviewer's judgement of a pre-PR report, bound to the tree
// it re-ran (knowledge/decisions/reviewer-verdict.md).
type Verdict struct {
	Outcome        string `json:"outcome"`
	By             string `json:"by"`
	At             string `json:"at"`
	Notes          string `json:"notes,omitempty"`
	TreeHash       string `json:"treeHash,omitempty"`
	Head           string `json:"head,omitempty"`
	RerunStartedAt string `json:"rerunStartedAt,omitempty"`
}

// HasOpen reports whether any finding is still open.
func (r *Report) HasOpen() bool {
	for _, f := range r.Findings {
		if f.Status == StatusOpen {
			return true
		}
	}
	return false
}

// Validate checks the report against its schema.
func (r *Report) Validate() error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return err
	}
	return schemas.Validate(schemas.Report, doc)
}

// LatestPath is the path of the latest report for tier under root.
func LatestPath(root, tier string) string {
	return filepath.Join(root, Dir, tier+"-latest.json")
}

// Write validates r and writes it as the tier's latest report and into
// history. It returns the latest-report path.
func Write(root string, r *Report) (string, error) {
	return LatestPath(root, r.Run.Tier), store(root, r)
}

// HistoryPath is where r's copy in the history directory lives: one file
// per run, named by its tier and start time.
func HistoryPath(root string, r *Report) string {
	stamp := strings.NewReplacer(":", "", "-", "").Replace(r.Run.StartedAt)
	return filepath.Join(root, Dir, "history", r.Run.Tier+"-"+stamp+".json")
}

// store validates r and writes it to its history copy and then as the
// tier's latest report. History is the durable record: a later run replaces
// the latest report, so a classification, acknowledgement, or verdict kept
// only there would be lost.
func store(root string, r *Report) error {
	if err := r.Validate(); err != nil {
		return fmt.Errorf("refusing to write an invalid report: %w", err)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	hist := HistoryPath(root, r)
	if err := os.MkdirAll(filepath.Dir(hist), 0o755); err != nil {
		return err
	}
	if err := writeFile(hist, b); err != nil {
		return err
	}
	return writeFile(LatestPath(root, r.Run.Tier), b)
}

// writeFile replaces path through a temporary file, so a reader never sees
// half a report.
func writeFile(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Read loads and validates a report.
func Read(path string) (*Report, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &r, nil
}

// Now formats t the way reports record timestamps.
func Now(t time.Time) string { return t.Format(time.RFC3339) }

// AgentKinds are the kinds the agent resolves itself; HumanKinds stop for a
// person. See knowledge/concepts/finding-kind.md.
var (
	AgentKinds = []string{KindUnclassified, KindNewCodeDefect, KindHarnessDefect, KindWrongProperty}
	HumanKinds = []string{KindLegacyDefectOrQuirk, KindCharacterizationDiff, KindEquivalentMutant, KindRequirementSilent, KindEnvironment, KindBelowThreshold}
)

// NeedsAgent reports whether f is open and the agent must act on it.
func NeedsAgent(f Finding) bool { return f.Status == StatusOpen && contains(AgentKinds, f.Kind) }

// NeedsHuman reports whether f is open and waits for a person.
func NeedsHuman(f Finding) bool { return f.Status == StatusOpen && contains(HumanKinds, f.Kind) }

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Latest returns the most recently started latest report among tiers, or
// nil when none exists. Unreadable reports are skipped.
func Latest(root string, tiers ...string) *Report {
	var best *Report
	for _, t := range tiers {
		r, err := Read(LatestPath(root, t))
		if err != nil {
			continue
		}
		if best == nil || startTime(r).After(startTime(best)) {
			best = r
		}
	}
	return best
}

func startTime(r *Report) time.Time {
	t, _ := time.Parse(time.RFC3339, r.Run.StartedAt)
	return t
}

// Rewrite replaces the tier's latest report and its history copy in place,
// after validation: classifying, acknowledging, and recording a verdict
// change the run's record, not add a run.
func Rewrite(root string, r *Report) error { return store(root, r) }

// RewriteHistory replaces only r's history copy, for a run that is no
// longer its tier's latest.
func RewriteHistory(root string, r *Report) error {
	if err := r.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(HistoryPath(root, r), append(b, '\n'))
}

// History returns the reports in the history directory, oldest first.
// Unreadable files are skipped.
func History(root string) []*Report {
	paths, _ := filepath.Glob(filepath.Join(root, Dir, "history", "*.json"))
	var out []*Report
	for _, p := range paths {
		if r, err := Read(p); err == nil {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return startTime(out[i]).Before(startTime(out[j])) })
	return out
}
