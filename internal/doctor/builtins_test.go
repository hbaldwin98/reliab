package doctor

import (
	"testing"

	"github.com/hbaldwin98/reliab/internal/profile"
)

// A command that starts with a shell builtin such as cd is not a missing
// tool.
func TestToolsShellBuiltin(t *testing.T) {
	p := &profile.Profile{Commands: map[string]profile.Command{
		"mutation_full": {Run: "cd tests && dotnet stryker"},
		"unit":          {Run: "no-such-tool-anywhere run"},
	}}
	got := map[string]Check{}
	for _, c := range tools(p) {
		got[c.Name] = c
	}
	if c := got["command mutation_full"]; c.Status != OK || c.Detail != "cd (shell builtin)" {
		t.Errorf("cd = %+v, want ok as a builtin", c)
	}
	if c := got["command unit"]; c.Status != Warn {
		t.Errorf("missing tool = %+v, want a warning", c)
	}
}

// knowledge/decisions/schema-v0-shape.md: doctor warns about each role the
// codebase needs but has no tool for, in role order.
func TestMissingRoles(t *testing.T) {
	no := false
	p := &profile.Profile{Roles: map[string]profile.Role{
		"T10": {Missing: true, Reason: "no store fault seam yet"},
		"T2":  {Missing: true, Reason: "no property library yet"},
		"T3":  {Applicable: &no, Reason: "no snapshots"},
		"T1":  {Tool: "go test"},
	}}
	got := missingRoles(p)
	if len(got) != 2 || got[0].Name != "role T2" || got[1].Name != "role T10" || got[0].Status != Warn || got[0].Detail != "needed but missing: no property library yet" {
		t.Errorf("missing roles = %+v", got)
	}
}
