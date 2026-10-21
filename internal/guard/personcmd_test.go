package guard

import (
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
)

// Cobra's command path (cobra user guide: commands and flags): the words
// that are not flags, where the word after a --flag with no = is that
// flag's value, never a command; a flag with = takes none. Only possible
// values may come before or between the path's words.
func TestCommandPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"--repo", ".", "findings", "ack"}, "findings ack"},
		{[]string{"findings", "--by", "x", "ack"}, "findings ack"},
		{[]string{"-C", "dir", "approve", "p"}, "approve"},
		{[]string{".", "findings", "ack"}, ""},
		{[]string{"findings", "x", "ack"}, ""},
		{[]string{"findings", "--by=x", "y", "ack"}, ""},
		{[]string{"report"}, ""},
		{[]string{"report", "route", "verdict"}, ""},
		{[]string{"baseline", "write"}, "baseline write"},
	}
	for _, tt := range tests {
		if got := commandPath(tt.args); got != tt.want {
			t.Errorf("commandPath(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

// knowledge/decisions/hook-decisions-v0.md: deny reliab findings ack,
// approve, and baseline write to agents, and report verdict to all but the
// reviewer, however the line spells them. The shell removes quotes and
// escapes before running a word (GNU Bash manual §3.1.2 Quoting, §3.5.9
// Quote Removal; PowerShell about_Quoting_Rules and about_Special_Characters:
// a backtick escapes the next character); cobra finds a command path among
// the words that are not flags, global flags included, and takes the word
// after a flag as its value (cobra user guide: flags); a variable may hold
// the binary's path. A review of v0.1.3 got each of the first four past
// the guard.
func TestPreBashPersonOnlyHoweverSpelled(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	bash := func(cmd string) hookio.Decision {
		return r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}})
	}
	pwsh := func(cmd string) hookio.Decision {
		return r.PreBash(hookio.Input{ToolName: "PowerShell", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}})
	}
	reviewer := func(cmd string) hookio.Decision {
		return r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, AgentType: ReviewerAgent, ToolInput: hookio.ToolInput{Command: cmd}})
	}
	tests := []struct {
		run  func(string) hookio.Decision
		cmd  string
		deny bool
	}{
		{bash, `reliab "findings" ack F-1 --by human:x`, true},
		{bash, `reli\ab findings ack F-1 --by human:x`, true},
		{bash, `reliab --repo . findings ack F-1 --by human:x`, true},
		{bash, `$r findings ack F-1 --by human:x`, true},
		{bash, `reliab findings --by human:x ack F-1`, true},
		{bash, `"$R" findings ack F-1`, true},
		{bash, `~/.reliab/bin/reliab.exe fin""dings ack F-1`, true},
		{bash, `go run ./cmd/reliab findings ack F-1`, true},
		{bash, `env reliab "approve" snapshots/s.txt --by human:x`, true},
		{bash, `reliab baseline 'write'`, true},
		{bash, `reliab report "verdict" --pass`, true},
		{reviewer, `reliab report "verdict" --pass`, false},
		{pwsh, `& "$env:USERPROFILE\.reliab\dev\reliab.exe" "findings" ack F-1 --by human:x`, true},
		{pwsh, "reli`ab findings ack F-1", true},
		{pwsh, `& $r findings ack F-1`, true},
		{pwsh, `Reliab.EXE Findings Ack F-1`, true},

		{bash, `reliab findings classify F-1 --kind requirement-silent --reason ack`, false},
		{bash, `reliab report route`, false},
		{bash, `reliab findings check out.json`, false},
		{bash, `git commit -m 'reliab "findings" ack is for a person'`, false},
		{bash, `echo $r findings ack`, false},
		{pwsh, `reliab report summary`, false},
	}
	for _, tt := range tests {
		if d := tt.run(tt.cmd); (d.Deny != "") != tt.deny {
			t.Errorf("%q: deny=%q, want deny=%v", tt.cmd, d.Deny, tt.deny)
		}
	}
}
