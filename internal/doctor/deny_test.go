package doctor

import (
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// knowledge/decisions/protected-path-approval.md: the profile guards itself
// once committed; until then the agent onboarding the repository writes it,
// so it has no deny rule. Other protected globs always do.
func TestDenyRulesCoverTheProfileOnceCommitted(t *testing.T) {
	p := &profile.Profile{ProtectedPaths: []string{"snapshots/**", profile.DefaultPath}}
	root := t.TempDir()
	testrepo.Git(t, root, "init", "-q", "-b", "main")
	testrepo.Write(t, root, profile.DefaultPath, "name: x\n")
	if got := strings.Join(DenyRules(root, p), ","); got != "Edit(/.reliab/**),Edit(/snapshots/**)" {
		t.Errorf("uncommitted: %s", got)
	}
	testrepo.Git(t, root, "add", "-A")
	testrepo.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "onboard")
	if got := strings.Join(DenyRules(root, p), ","); got != "Edit(/.reliab/**),Edit(/snapshots/**),Edit(/reliability-profile.yaml)" {
		t.Errorf("committed: %s", got)
	}
}
