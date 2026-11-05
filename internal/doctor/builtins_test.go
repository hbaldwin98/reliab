package doctor

import (
	"testing"

	"github.com/hbaldwin98/reliab/internal/runner"
)

// Source: knowledge/decisions/profile-v2.md (reliab.yaml commands run under
// sh): a line that starts with a shell builtin such as cd is not a missing
// tool, and a tool shared by several context lines is checked once.
func TestToolsShellBuiltin(t *testing.T) {
	cfg, err := runner.Parse([]byte(`version: 1
commands:
  mutation: { about: x, pr: cd tests && dotnet stryker, audit: cd tests && dotnet stryker }
  unit: { about: x, change: no-such-tool-anywhere run }
`))
	if err != nil {
		t.Fatal(err)
	}
	checks := tools(cfg)
	if len(checks) != 2 {
		t.Fatalf("checks = %+v, want one per distinct program", checks)
	}
	got := map[string]Check{}
	for _, c := range checks {
		got[c.Name] = c
	}
	if c := got["command mutation"]; c.Status != OK || c.Detail != "cd (shell builtin)" {
		t.Errorf("cd = %+v, want ok as a builtin", c)
	}
	if c := got["command unit"]; c.Status != Warn {
		t.Errorf("missing tool = %+v, want a warning", c)
	}
}
