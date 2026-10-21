package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/guard"
	"github.com/hbaldwin98/reliab/internal/hookio"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

func read(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInitNewRepo(t *testing.T) {
	root := t.TempDir()
	testrepo.Git(t, root, "init", "-q", "-b", "main")
	if _, _, err := Init(root, Options{}); err == nil {
		t.Fatal("a new profile was written without a starting condition")
	}
	done, p, err := Init(root, Options{Condition: "maintained", Name: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "svc" || p.StartingCondition != "maintained" || len(done) < 4 {
		t.Fatalf("profile %+v, changes %v", p, done)
	}
	if !strings.Contains(read(t, root, "AGENTS.md"), "Never change an assertion") {
		t.Error("AGENTS.md lacks the rules")
	}
	if !strings.Contains(read(t, root, ".gitignore"), ".reliab/") {
		t.Error(".gitignore lacks .reliab/")
	}

	again, _, err := Init(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || !strings.Contains(again[0], "dev build") {
		t.Fatalf("second run changed things: %v", again)
	}
}

func TestInitMergesExistingFiles(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\"snapshots/**\", \"**/golden/**\"]\ncommands:\n  build: \"true\"\n")
	testrepo.Write(t, root, ".claude/settings.json", `{"model": "x", "permissions": {"deny": ["Bash(rm -rf /)"], "allow": ["Bash(reliab *)"]}}`)
	testrepo.Write(t, root, "AGENTS.md", "# Project\n\nKeep this.\n")

	if _, _, err := Init(root, Options{}); err != nil {
		t.Fatal(err)
	}

	var s struct {
		Model       string `json:"model"`
		Permissions struct {
			Deny, Allow []string
		} `json:"permissions"`
		EnabledPlugins map[string]bool `json:"enabledPlugins"`
		Sandbox        struct {
			Enabled    *bool `json:"enabled"`
			Filesystem struct {
				DenyWrite []string `json:"denyWrite"`
			} `json:"filesystem"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal([]byte(read(t, root, ".claude/settings.json")), &s); err != nil {
		t.Fatal(err)
	}
	if s.Model != "x" || s.Permissions.Deny[0] != "Bash(rm -rf /)" {
		t.Errorf("existing settings lost: %+v", s)
	}
	// knowledge/decisions/light-guardrails.md: init adds no deny rules; the
	// user's own stay.
	if strings.Join(s.Permissions.Deny, ",") != "Bash(rm -rf /)" {
		t.Errorf("deny = %v, want only the user's rule", s.Permissions.Deny)
	}
	if len(s.Permissions.Allow) != 1 || !s.EnabledPlugins[PluginID] {
		t.Errorf("allow %v, plugins %v", s.Permissions.Allow, s.EnabledPlugins)
	}
	if s.Sandbox.Enabled != nil || len(s.Sandbox.Filesystem.DenyWrite) != 0 {
		t.Errorf("sandbox = %+v; init must not touch it", s.Sandbox)
	}

	agents := read(t, root, "AGENTS.md")
	if !strings.HasPrefix(agents, "# Project\n\nKeep this.\n") || strings.Count(agents, agentsBegin) != 1 {
		t.Errorf("AGENTS.md = %q", agents)
	}
	if _, _, err := Init(root, Options{}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(read(t, root, "AGENTS.md"), agentsBegin) != 1 {
		t.Error("a second init duplicated the rules section")
	}

}

// knowledge/decisions/protected-path-approval.md: the profile init writes
// is protected once the person commits it. Until then the agent onboarding
// the repository writes it; after, the pre-edit hook reminds the agent.
func TestInitProtectsTheProfileOnceCommitted(t *testing.T) {
	root := t.TempDir()
	testrepo.Git(t, root, "init", "-q", "-b", "main")
	_, p, err := Init(root, Options{Condition: "greenfield", Name: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.ProtectedPaths, ",") != "reliability-profile.yaml" {
		t.Fatalf("protected_paths = %v, want the profile", p.ProtectedPaths)
	}
	editProfile := func() hookio.Decision {
		t.Helper()
		r, ok, err := guard.Open(root)
		if err != nil || !ok {
			t.Fatalf("open: %v %v", ok, err)
		}
		return r.PreEdit(hookio.Input{Cwd: root, ToolInput: hookio.ToolInput{FilePath: "reliability-profile.yaml"}})
	}
	if d := editProfile(); d != (hookio.Decision{}) {
		t.Errorf("uncommitted profile: %+v", d)
	}

	testrepo.Git(t, root, "add", "-A")
	testrepo.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "onboard")
	if d := editProfile(); d.Deny != "" || d.Context == "" {
		t.Errorf("committed profile: %+v, want a reminder", d)
	}
}
