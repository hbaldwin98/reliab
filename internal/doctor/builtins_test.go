package doctor

import (
	"strings"
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

// knowledge/decisions/debug-adapter-seam.md: reliab doctor checks that the
// debug adapter's first word is on PATH when the section is present; as a
// missing tool does, a missing adapter warns.
func TestDebugAdapter(t *testing.T) {
	check := func(run string) []Check {
		return debugAdapter(&profile.Profile{Debug: &profile.Debug{Adapter: profile.DebugAdapter{Run: run, Transport: "stdio"}}})
	}
	if got := debugAdapter(&profile.Profile{}); got != nil {
		t.Errorf("no debug section: %+v", got)
	}
	if got := check("no-such-adapter-anywhere --interpreter=vscode"); len(got) != 1 || got[0].Status != Warn || !strings.Contains(got[0].Detail, "no-such-adapter-anywhere is not on PATH") {
		t.Errorf("missing adapter = %+v", got)
	}
	if got := check("git dap"); len(got) != 1 || got[0].Status != OK || !strings.HasPrefix(got[0].Detail, "git (stdio)") {
		t.Errorf("adapter on PATH = %+v", got)
	}
	if got := check("   "); len(got) != 1 || got[0].Status != Fail || !strings.Contains(got[0].Detail, "empty") {
		t.Errorf("blank run = %+v", got)
	}
	if got := check(`"unclosed`); len(got) != 1 || got[0].Status != Fail {
		t.Errorf("bad run = %+v", got)
	}
	if got := check("{root}/bin/adapter"); len(got) != 1 || got[0].Status != OK || !strings.Contains(got[0].Detail, "placeholder") {
		t.Errorf("placeholder = %+v", got)
	}
}
