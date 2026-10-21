package repocheck

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/templates"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// The repository's AGENTS.md carries the same testing rules profile init
// writes into a consumer's (templates/agents.md, between the markers
// internal/setup's ensureAgents uses); they were once synced by hand.
func TestAgentsBlockMatchesTemplate(t *testing.T) {
	text := readFile(t, "AGENTS.md")
	const begin, end = "<!-- reliab:begin -->", "<!-- reliab:end -->"
	i, j := strings.Index(text, begin), strings.Index(text, end)
	if i < 0 || j < i {
		t.Fatalf("AGENTS.md has no %s ... %s block", begin, end)
	}
	have := strings.TrimSpace(text[i+len(begin) : j])
	want := strings.TrimSpace(strings.ReplaceAll(templates.Agents, "\r\n", "\n"))
	if have != want {
		t.Error("AGENTS.md's reliab block differs from templates/agents.md; keep them identical")
	}
}

// The dev repo's project reviewer agent is the plugin's reviewer, byte for
// byte, so it cannot drift (knowledge/decisions/dev-environment.md).
func TestDevReviewerMatchesPlugin(t *testing.T) {
	a, err := os.ReadFile(filepath.Join(root, "plugin/agents/reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".claude/agents/reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error(".claude/agents/reviewer.md differs from plugin/agents/reviewer.md; copy the plugin's file over it")
	}
}

// Every adapter reliab ships is named where users look for it: the README
// and docs/adapters.md list the adapters that ship beside the binary
// (knowledge/decisions/external-adapters.md). The release config's builds
// are checked by TestReleaseArchivesHoldEverything.
func TestAdaptersDocumented(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(root, "cmd", "adapters"))
	if err != nil {
		t.Fatal(err)
	}
	readme, adapters := readFile(t, "README.md"), readFile(t, "docs/adapters.md")
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n++
		full := "`reliab-adapter-" + e.Name() + "`"
		if !strings.Contains(readme, full) {
			t.Errorf("README.md does not name %s", full)
		}
		// docs/adapters.md lists them as `reliab-adapter-a`, `-b`, ...
		if !strings.Contains(adapters, full) && !strings.Contains(adapters, "`-"+e.Name()+"`") {
			t.Errorf("docs/adapters.md does not name %s", full)
		}
	}
	if n == 0 {
		t.Fatal("no adapters under cmd/adapters")
	}
}

// Every hook script the project settings run exists, so a session in this
// repository does not start with a failing hook.
func TestProjectHookScriptsExist(t *testing.T) {
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string }
		}
	}
	readJSON(t, ".claude/settings.json", &s)
	re := regexp.MustCompile(`^(?:sh|awk -f) "\$CLAUDE_PROJECT_DIR/(\.claude/hooks/[\w.-]+\.(?:sh|awk))"$`)
	n := 0
	for event, groups := range s.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				m := re.FindStringSubmatch(h.Command)
				if m == nil {
					t.Errorf("%s: command %q does not run a script under .claude/hooks", event, h.Command)
					continue
				}
				if _, err := os.Stat(filepath.Join(root, m[1])); err != nil {
					t.Errorf("%s: %v", event, err)
				}
				n++
			}
		}
	}
	if n < 1 {
		t.Errorf("only %d project hooks configured", n)
	}
}
