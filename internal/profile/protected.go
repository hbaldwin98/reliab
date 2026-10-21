package profile

import "github.com/hbaldwin98/reliab/internal/gitx"

// AlwaysProtected are reliab's own outputs: only the binary writes them
// (knowledge/decisions/report-storage.md).
var AlwaysProtected = []string{".reliab/**"}

// EditSet is what no agent edits in a repository: reliab's outputs and the
// profile's protected paths, minus the profile itself while HEAD has no
// copy of it, since the profile being onboarded is the agent's to write
// until the person commits it
// (knowledge/decisions/protected-path-approval.md). The pre-edit hook
// reminds the agent of paths in it. reliab gate protected keeps its own
// matching of git's diff, which judges a change, not an edit.
type EditSet struct {
	root       string
	globs      []string
	onboarding int8 // 0: not asked yet; 1: HEAD has no profile; -1: it has
}

// NewEditSet returns the edit-time protected set of the repository at root
// whose profile is p. It runs nothing until asked.
func NewEditSet(root string, p *Profile) *EditSet {
	return &EditSet{root: root, globs: append(append([]string{}, AlwaysProtected...), p.ProtectedPaths...)}
}

// Globs returns reliab's outputs and the profile's protected paths.
func (s *EditSet) Globs() []string { return s.globs }

// Exempt reports whether rel, a repo-relative slash path, is the profile
// while it is still the agent's to write. It asks git only for the
// profile's own path, and once.
func (s *EditSet) Exempt(rel string) bool {
	if rel != DefaultPath {
		return false
	}
	if s.onboarding == 0 {
		s.onboarding = -1
		if gitx.BlobAt(s.root, "HEAD", DefaultPath) == "" {
			s.onboarding = 1
		}
	}
	return s.onboarding == 1
}
