package gitx

import (
	"path/filepath"
	"strings"
)

// Rel returns p, an absolute path, relative to root as a slash path; ok is
// false when p is outside root. When the plain comparison says outside, both
// sides are compared again with symlinks and Windows short names resolved, so
// /var and /private/var (macOS) or RUNNER~1 and runneradmin (Windows) name
// the same directory. Without that, a protected file named through such an
// alias would look outside the repository and go unguarded.
func Rel(root, p string) (string, bool) {
	if rel, ok := within(root, p); ok {
		return rel, true
	}
	return within(realPath(root), realPath(p))
}

func within(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// realPath resolves the longest existing prefix of p, so a file not yet
// created still resolves through its directory.
func realPath(p string) string {
	p = filepath.Clean(p)
	var rest []string
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(append([]string{r}, rest...)...)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(append([]string{p}, rest...)...)
		}
		rest = append([]string{filepath.Base(p)}, rest...)
		p = parent
	}
}
