package effects

import (
	"slices"
	"strings"
	"testing"
)

// knowledge/decisions/hook-decisions-v0.md: an option naming a file for
// output is a write target: git-diff(1) --output=<file> or --output <file>;
// Invoke-WebRequest -OutFile <path> or, as PowerShell binds any parameter,
// -OutFile:<path> (about_Parameters); git-format-patch(1) -o <dir>, the
// value attached or not (gitcli(7): a short option's value may be stuck);
// curl(1) -o. git-ls-files(1) -o is --others and names no output, and an
// option with no word after it names nothing. (Moved from the guard's
// outputs test.)
func TestWritesFromOutputOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line string
		pwsh bool
		want []string
	}{
		{"git diff --output=Snap/x", false, []string{"Snap/x"}},
		{"git diff --output Snap/x", false, []string{"Snap/x"}},
		{`Invoke-WebRequest u -OutFile:Snap\x`, true, []string{`Snap\x`}},
		{"Invoke-WebRequest u -OutFile x", true, []string{"x"}},
		{"git format-patch -oDir", false, []string{"Dir"}},
		{"git format-patch -o Dir", false, []string{"Dir"}},
		{"curl -o x u", false, []string{"x"}},
		{"git ls-files -o x", false, nil},
		{"echo -oops x", false, nil},
		{"curl u -o", false, nil},
		{"git diff --output", false, nil},
	}
	for _, tt := range tests {
		e := Read(tt.line, bashOrPwsh(tt.pwsh), t.TempDir())
		if got := e.Commands[0].Writes; !slices.Equal(got, tt.want) {
			t.Errorf("%q writes %q, want %q", tt.line, got, tt.want)
		}
	}
}

// Cobra's command path (cobra user guide: commands and flags): the words
// that are not flags, where the word after a --flag with no = is that
// flag's value, never a command; a flag with = takes none. Only possible
// values may come before or between the path's words. (Moved from the
// guard's commandPath test.)
func TestInvokesCommandPath(t *testing.T) {
	t.Parallel()
	paths := [][]string{{"findings", "ack"}, {"approve"}, {"baseline", "write"}, {"report", "verdict"}}
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
		e := Read(strings.Join(append([]string{"reliab"}, tt.args...), " "), Bash, t.TempDir())
		got := ""
		for _, p := range paths {
			if e.Invokes(p...) {
				got = strings.Join(p, " ")
			}
		}
		if got != tt.want {
			t.Errorf("reliab %q invokes %q, want %q", tt.args, got, tt.want)
		}
	}
}
