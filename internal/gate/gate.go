// Package gate holds the agent-free CI gates that are not a verify tier.
package gate

import (
	"fmt"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/hbaldwin98/reliab/internal/approval"
	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
)

// ProtectedChange is a changed file, the protected glob it matched, and the
// local approval of its current content, if a person gave one.
type ProtectedChange struct {
	Path     string             `json:"path"`
	Glob     string             `json:"glob"`
	Approval *approval.Approval `json:"approval,omitempty"`

	baseBlob, blob string
}

// Protected lists protected files a branch changed relative to ref, each
// with the approval that matches its change, if any. The CI host turns an
// unapproved change into a required human approval
// (knowledge/decisions/ci-agnostic.md); the approvals are local, so on CI
// every change is unapproved.
//
// The globs are the profile's and its base version's: a branch that drops a
// glob from protected_paths, or the profile's own entry, cannot hide its
// changes from the gate (knowledge/decisions/protected-path-approval.md).
func Protected(root string, p *profile.Profile, ref string) ([]ProtectedChange, error) {
	mb, err := gitx.MergeBase(root, ref)
	if err != nil {
		return nil, err
	}
	globs := p.ProtectedPaths
	if raw, err := gitx.FileAt(root, mb, profile.DefaultPath); err == nil {
		base, err := profile.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("the profile at the merge base with %s: %w", ref, err)
		}
		globs = append(append([]string{}, globs...), base.ProtectedPaths...)
	}
	changed, err := gitx.ChangedSince(root, ref)
	if err != nil {
		return nil, err
	}
	approvals, err := approval.Load(root)
	if err != nil {
		return nil, err
	}
	var out []ProtectedChange
	for _, f := range changed {
		for _, g := range globs {
			if ok, _ := doublestar.Match(g, f); ok {
				c := ProtectedChange{Path: f, Glob: g, baseBlob: gitx.BlobAt(root, mb, f)}
				if c.blob, err = gitx.WorkingBlob(root, f); err != nil {
					return nil, err
				}
				c.Approval = approval.Match(approvals, f, c.baseBlob, c.blob)
				out = append(out, c)
				break
			}
		}
	}
	return out, nil
}

// Unapproved returns the changes no approval matches.
func Unapproved(cs []ProtectedChange) []ProtectedChange {
	var out []ProtectedChange
	for _, c := range cs {
		if c.Approval == nil {
			out = append(out, c)
		}
	}
	return out
}

// Approve records that by, a person, approved the current content of each
// of paths: repo-relative protected files the branch changed since ref.
// Hooks deny the command to agents; a later edit to a file voids its
// approval.
func Approve(root string, p *profile.Profile, ref string, paths []string, by string, now time.Time) ([]approval.Approval, error) {
	if !strings.HasPrefix(by, "human:") || strings.TrimSpace(strings.TrimPrefix(by, "human:")) == "" {
		return nil, fmt.Errorf("--by %q: want human:<name>; only a person approves a protected change", by)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("name the protected files to approve")
	}
	mb, err := gitx.MergeBase(root, ref)
	if err != nil {
		return nil, err
	}
	changes, err := Protected(root, p, ref)
	if err != nil {
		return nil, err
	}
	byPath := map[string]ProtectedChange{}
	for _, c := range changes {
		byPath[c.Path] = c
	}
	all, err := approval.Load(root)
	if err != nil {
		return nil, err
	}
	var added []approval.Approval
	for _, path := range paths {
		c, ok := byPath[path]
		if !ok {
			return nil, fmt.Errorf("%s is not a protected file changed since %s; reliab gate protected --base %s lists them", path, ref, ref)
		}
		a := approval.Approval{
			Path: path, Blob: c.blob, BaseBlob: c.baseBlob, Base: ref, MergeBase: mb, By: by, At: report.Now(now),
		}
		all = approval.Put(all, a)
		added = append(added, a)
	}
	return added, approval.Save(root, all)
}
