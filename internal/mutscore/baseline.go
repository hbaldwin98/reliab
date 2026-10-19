package mutscore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/schemas"
)

// BaselineVersion is the baseline schema version this build writes.
const BaselineVersion = "0.1.0"

// Baseline is the file the profile's mutation_baseline names
// (schemas/baseline.schema.json): a recorded score per scope that mutation
// runs must not fall below. It is a protected path; only a person changes it.
type Baseline struct {
	SchemaVersion string  `json:"schemaVersion"`
	Scopes        []Scope `json:"scopes"`
}

// Scope is one area's recorded score.
type Scope struct {
	Path       string  `json:"path"`
	Score      float64 `json:"score"`
	Killed     int     `json:"killed,omitempty"`
	Counted    int     `json:"counted,omitempty"`
	Command    string  `json:"command,omitempty"`
	RecordedAt string  `json:"recordedAt,omitempty"`
	TreeHash   string  `json:"treeHash,omitempty"`
}

// LoadBaseline reads the profile's baseline file. It returns nil when the
// profile names none or the file is not written yet.
func LoadBaseline(root string, p *profile.Profile) (*Baseline, error) {
	if p.MutationBaseline == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p.MutationBaseline)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := ParseBaseline(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.MutationBaseline, err)
	}
	return b, nil
}

// ParseBaseline validates and decodes a baseline file.
func ParseBaseline(raw []byte) (*Baseline, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if err := schemas.Validate(schemas.Baseline, doc); err != nil {
		return nil, err
	}
	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	for _, s := range b.Scopes {
		if !doublestar.ValidatePattern(s.Path) {
			return nil, fmt.Errorf("scope %q is not a valid glob", s.Path)
		}
	}
	return &b, nil
}

// Change is one scope's recorded score before and after a proposal. Old is
// nil for a new scope.
type Change struct {
	Path string
	Old  *float64
	New  Scope
}

// Lower reports whether the change lowers a recorded score.
func (c Change) Lower() bool { return c.Old != nil && c.New.Score < *c.Old }

// Propose scores each scope from a mutation command's result in rep and
// returns the baseline with those scopes recorded; scopes it does not name
// keep their recorded score. With no scopes given it re-records the
// existing ones.
func Propose(cur *Baseline, rep *report.Report, command string, scopes []string, now time.Time) (*Baseline, []Change, error) {
	var res *report.CommandResult
	for i := range rep.Run.Commands {
		if rep.Run.Commands[i].ID == command {
			res = &rep.Run.Commands[i]
		}
	}
	if res == nil {
		return nil, nil, fmt.Errorf("the %s report did not run %s", rep.Run.Tier, command)
	}
	if res.MutantFiles == nil {
		return nil, nil, fmt.Errorf("%s reported no per-file mutant counts, so no scope can be scored", command)
	}
	next := &Baseline{SchemaVersion: BaselineVersion}
	old := map[string]float64{}
	// Re-recording every scope keeps one this run measured nothing in; a
	// scope named on purpose must have mutants.
	defaulted := false
	if cur != nil {
		next.Scopes = append(next.Scopes, cur.Scopes...)
		for _, s := range cur.Scopes {
			old[s.Path] = s.Score
		}
		if len(scopes) == 0 {
			for _, s := range cur.Scopes {
				scopes = append(scopes, s.Path)
			}
			defaulted = true
		}
	}
	if len(scopes) == 0 {
		return nil, nil, errors.New("name at least one scope (a glob such as src/Billing/**)")
	}
	confirmed := map[string]int{}
	for _, f := range rep.Findings {
		if f.Command == command && f.Kind == report.KindEquivalentMutant && f.Status == report.StatusAcknowledged {
			confirmed[fileOf(f.Location)]++
		}
	}
	var changes []Change
	for _, path := range scopes {
		if !doublestar.ValidatePattern(path) {
			return nil, nil, fmt.Errorf("scope %q is not a valid glob", path)
		}
		killed, counted := inScope(path, res.MutantFiles, confirmed)
		if counted == 0 && defaulted {
			continue
		}
		if counted == 0 {
			return nil, nil, fmt.Errorf("scope %s: %s measured no mutants there", path, command)
		}
		s := Scope{
			Path: path, Score: floor1(report.Percent(killed, counted)), Killed: killed, Counted: counted,
			Command: command, RecordedAt: report.Now(now), TreeHash: rep.Run.TreeHash,
		}
		ch := Change{Path: path, New: s}
		if o, ok := old[path]; ok {
			ch.Old = &o
		}
		changes = append(changes, ch)
		replaced := false
		for i := range next.Scopes {
			if next.Scopes[i].Path == path {
				next.Scopes[i], replaced = s, true
			}
		}
		if !replaced {
			next.Scopes = append(next.Scopes, s)
		}
	}
	sort.Slice(next.Scopes, func(i, j int) bool { return next.Scopes[i].Path < next.Scopes[j].Path })
	return next, changes, nil
}

// floor1 rounds a percentage down to one decimal, so a recorded score never
// claims more than was measured.
func floor1(f float64) float64 { return float64(int64(f*10)) / 10 }

// Marshal renders a baseline file.
func (b *Baseline) Marshal() ([]byte, error) {
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, err
	}
	if _, err := ParseBaseline(raw); err != nil {
		return nil, fmt.Errorf("refusing to write an invalid baseline: %w", err)
	}
	return append(raw, '\n'), nil
}
