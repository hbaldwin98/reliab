// Package characterize captures an area's behavior before a refactor or
// migration, captures it again after, and compares the two
// (knowledge/decisions/on-demand-characterization.md). Captures live under
// .reliab/, are never committed, and need no approval: they are thrown away
// once the change is done.
package characterize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hbaldwin98/reliab/internal/adapters"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/verify"
	"github.com/hbaldwin98/reliab/pkg/snapdiff"
)

// Dir holds the captures, relative to the repo root. Only reliab writes it.
const Dir = ".reliab/characterize"

const (
	baselineDir = "baseline"
	currentDir  = "current"
	metaFile    = "baseline.json"
)

// Meta records what a baseline capture covered.
type Meta struct {
	Scope      []string `json:"scope,omitempty"`
	TreeHash   string   `json:"treeHash"`
	CapturedAt string   `json:"capturedAt"`
	Files      int      `json:"files"`
}

// Capture runs the characterize command over opt.Scope into a baseline.
// It refuses while a baseline exists: capturing again after the change
// would make the changed behavior the baseline, approving every difference
// unseen. Command failures are findings in the returned report, and leave
// no baseline behind.
func Capture(ctx context.Context, opt verify.Options) (*report.Report, string, error) {
	if m, err := ReadMeta(opt.Root); err == nil {
		return nil, "", fmt.Errorf("a capture from %s already exists: compare against it, or run reliab characterize discard first", m.CapturedAt)
	}
	root := filepath.Join(opt.Root, Dir)
	if err := os.RemoveAll(root); err != nil {
		return nil, "", err
	}
	dir := filepath.Join(root, baselineDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	opt.Only = []string{verify.CharacterizeID}
	opt.Characterize = &verify.Characterize{Dir: dir, Judge: func(exit int, logTail string) []report.Finding {
		if exit != 0 {
			return []report.Finding{failure(fmt.Sprintf("capture failed: exit %d\n%s", exit, logTail))}
		}
		if n, err := count(dir); err != nil || n == 0 {
			return []report.Finding{failure(fmt.Sprintf("the characterize command wrote nothing into %s; it must write its transcripts to {capture} (RELIAB_CAPTURE_DIR)", Dir+"/"+baselineDir))}
		}
		return nil
	}}
	rep, path, err := verify.Run(ctx, opt)
	if err != nil {
		_ = os.RemoveAll(root)
		return nil, "", err
	}
	if rep.HasOpen() {
		return rep, path, os.RemoveAll(root)
	}
	n, err := count(dir)
	if err != nil {
		return nil, "", err
	}
	raw, err := json.MarshalIndent(Meta{Scope: rep.Run.Scope, TreeHash: rep.Run.TreeHash, CapturedAt: rep.Run.StartedAt, Files: n}, "", "  ")
	if err != nil {
		return nil, "", err
	}
	return rep, path, os.WriteFile(filepath.Join(root, metaFile), append(raw, '\n'), 0o644)
}

// ReadMeta returns the current baseline's metadata.
func ReadMeta(repoRoot string) (*Meta, error) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, Dir, metaFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("no capture to compare with: run reliab characterize capture before the change")
	}
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", metaFile, err)
	}
	return &m, nil
}

// Compare captures the baseline's scope again and reports each transcript
// that differs from the baseline as a labelled finding. opt.Scope is
// ignored: the comparison covers what was captured.
func Compare(ctx context.Context, opt verify.Options) (*report.Report, string, error) {
	m, err := ReadMeta(opt.Root)
	if err != nil {
		return nil, "", err
	}
	base := filepath.Join(opt.Root, Dir, baselineDir)
	cur := filepath.Join(opt.Root, Dir, currentDir)
	if err := os.RemoveAll(cur); err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(cur, 0o755); err != nil {
		return nil, "", err
	}
	opt.Only = []string{verify.CharacterizeID}
	opt.Scope = m.Scope
	opt.Characterize = &verify.Characterize{Dir: cur, Judge: func(exit int, logTail string) []report.Finding {
		if exit != 0 {
			return []report.Finding{failure(fmt.Sprintf("capture after the change failed: exit %d\n%s", exit, logTail))}
		}
		fs, err := Diff(base, cur)
		if err != nil {
			return []report.Finding{failure("cannot compare captures: " + err.Error())}
		}
		return fs
	}}
	return verify.Run(ctx, opt)
}

// Discard removes every capture.
func Discard(repoRoot string) error { return os.RemoveAll(filepath.Join(repoRoot, Dir)) }

// Diff compares two capture directories file by file. A file in both that
// differs, or in only one, is a finding labelled by pkg/snapdiff; a
// file that disappeared counts as removed lines.
func Diff(baseline, current string) ([]report.Finding, error) {
	before, err := files(baseline)
	if err != nil {
		return nil, err
	}
	after, err := files(current)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for n := range before {
		names[n] = true
	}
	for n := range after {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	var out []report.Finding
	for _, name := range sorted {
		b, inBefore := before[name]
		a, inAfter := after[name]
		head := "capture changed"
		switch {
		case !inBefore:
			head = "new in the capture after the change"
		case !inAfter:
			head = "missing from the capture after the change"
		}
		r := snapdiff.Compare(b, a)
		if r.Label == "" && inBefore == inAfter {
			continue
		}
		// An empty file that appeared or disappeared has no lines to diff.
		if r.Label == "" && inBefore {
			r.Label = snapdiff.ValuesChanged
		} else if r.Label == "" {
			r.Label = snapdiff.AdditiveOnly
		}
		out = append(out, report.Finding{
			ID:       adapters.ID(verify.CharacterizeID, name),
			Kind:     report.KindUnclassified,
			Command:  verify.CharacterizeID,
			Test:     name,
			Location: Dir + "/" + currentDir + "/" + name,
			Message:  truncate(fmt.Sprintf("%s: %s (+%d -%d)\n%s", head, snapdiff.Describe(r.Label), r.Added, r.Removed, r.Diff)),
			Diff:     r.Label,
			Status:   report.StatusOpen,
		})
	}
	return out, nil
}

// files reads every file under dir, keyed by slash-separated relative path.
func files(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	return out, err
}

func count(dir string) (int, error) {
	fs, err := files(dir)
	return len(fs), err
}

func failure(msg string) report.Finding {
	return report.Finding{
		ID:      adapters.ID(verify.CharacterizeID, ""),
		Kind:    report.KindUnclassified,
		Command: verify.CharacterizeID,
		Message: truncate(msg),
		Status:  report.StatusOpen,
	}
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= adapters.MaxMessage {
		return s
	}
	return s[:adapters.MaxMessage] + "…(truncated)"
}
