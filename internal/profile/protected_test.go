package profile

import (
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// knowledge/decisions/report-storage.md: reliab's outputs are protected in
// every repository; knowledge/decisions/protected-path-approval.md: the
// profile is exempt only while HEAD has no copy of it, and no other path
// ever is.
func TestEditSet(t *testing.T) {
	p := &Profile{ProtectedPaths: []string{"snapshots/**", DefaultPath}}
	root := t.TempDir()
	testrepo.Git(t, root, "init", "-q", "-b", "main")
	s := NewEditSet(root, p)
	if got := strings.Join(s.Globs(), ","); got != ".reliab/**,snapshots/**,"+DefaultPath {
		t.Errorf("globs = %s", got)
	}
	if !s.Exempt(DefaultPath) || s.Exempt("snapshots/s.txt") || s.Exempt(".reliab/x") {
		t.Errorf("uncommitted: exempt(profile)=%v exempt(snapshots/s.txt)=%v", s.Exempt(DefaultPath), s.Exempt("snapshots/s.txt"))
	}
	testrepo.Write(t, root, DefaultPath, "name: x\n")
	testrepo.Git(t, root, "add", "-A")
	testrepo.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "onboard")
	if NewEditSet(root, p).Exempt(DefaultPath) {
		t.Error("the committed profile is still exempt")
	}
}
