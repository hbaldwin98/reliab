package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/doctor"
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
	// The profile predates self-protection, so init adds it to
	// protected_paths, and the committed profile gets its deny rule
	// (knowledge/decisions/protected-path-approval.md).
	want := []string{"Bash(rm -rf /)", "Edit(/.reliab/**)", "Edit(/snapshots/**)", "Edit(**/golden/**)", "Edit(/reliability-profile.yaml)"}
	if strings.Join(s.Permissions.Deny, ",") != strings.Join(want, ",") {
		t.Errorf("deny = %v, want %v", s.Permissions.Deny, want)
	}
	if len(s.Permissions.Allow) != 1 || !s.EnabledPlugins[PluginID] {
		t.Errorf("allow %v, plugins %v", s.Permissions.Allow, s.EnabledPlugins)
	}
	if s.Sandbox.Enabled != nil || strings.Join(s.Sandbox.Filesystem.DenyWrite, ",") != "./snapshots" {
		t.Errorf("sandbox = %+v; init must not enable it and maps only dir/** globs", s.Sandbox)
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

	for _, c := range doctor.Run(root) {
		if c.Name == "deny rules" && c.Status != doctor.OK {
			t.Errorf("doctor after init: %+v", c)
		}
	}
}

// knowledge/decisions/protected-path-approval.md: the profile init writes
// guards itself once the person commits it. Until then the agent
// onboarding the repository writes it; after, the hooks and the deny rules
// cover it.
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
	const rule = "Edit(/reliability-profile.yaml)"
	if d := editProfile(); d.Deny != "" {
		t.Errorf("uncommitted profile denied: %s", d.Deny)
	}
	if strings.Contains(read(t, root, ".claude/settings.json"), rule) {
		t.Error("uncommitted profile has a deny rule")
	}

	testrepo.Git(t, root, "add", "-A")
	testrepo.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "onboard")
	if d := editProfile(); d.Deny == "" {
		t.Error("committed profile not denied")
	}
	asked := false
	for _, c := range doctor.Run(root) {
		asked = asked || c.Name == "deny rules" && c.Status == doctor.Warn && strings.Contains(c.Detail, rule)
	}
	if !asked {
		t.Errorf("doctor after the commit does not ask for %s", rule)
	}
	if _, _, err := Init(root, Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, root, ".claude/settings.json"), rule) {
		t.Errorf("re-run init did not add %s", rule)
	}
}
