package guard

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
)

// GNU Bash manual §3.2.4 Lists: commands separated by ; run in sequence,
// and after || the next runs when the one before failed; §4.1 cd changes
// the shell's working directory, which no separator undoes. So a cd stays
// in effect after ;, ||, and a newline, and one the hook cannot follow may
// lead anywhere (knowledge/decisions/hook-decisions-v0.md: a write to a
// protected path is denied wherever its simple command may run). A review
// of v0.1.3 allowed cd internal; rm ../reliability-profile.yaml.
func TestPartsMayRun(t *testing.T) {
	t.Parallel()
	session, out := scratch(t), scratch(t)
	o := filepath.ToSlash(out)
	tests := []struct {
		cmd  string
		part int
		want []string
	}{
		{"cd " + o + "; rm x", 1, []string{session, out}},
		{"cd " + o + "\nrm x", 1, []string{session, out}},
		{"cd " + o + " || exit; rm x", 2, []string{session, out}},
		{"cd " + o + " && true; rm x", 2, []string{session, out}},
		{"cd " + o + " && cd sub || rm x", 2, []string{session, out, filepath.Join(out, "sub")}},
		{"true || cd " + o + " && rm x", 2, []string{session, out}},
		// A cd surely run and joined by && moves what follows.
		{"cd " + o + " && rm x", 1, []string{out}},
		{"cd $D && rm x", 1, []string{session, anywhere}},
		{"cd $D; cd " + o + " && rm x", 2, []string{out, anywhere}},
		{"popd; rm x", 1, []string{session, anywhere}},
		// A line the hook cannot track: anywhere when it may change directory.
		{"cd " + o + "; rm x # tidy", 1, []string{session, anywhere}},
		{"source env.sh; rm x", 1, []string{session, anywhere}},
		{"echo $(date); rm x", 1, []string{session}},
	}
	for _, tt := range tests {
		ps := parts(session, tt.cmd, false)
		if tt.part >= len(ps) {
			t.Errorf("%q: %d parts, want more than %d", tt.cmd, len(ps), tt.part)
			continue
		}
		got := ps[tt.part].may
		if len(got) != len(tt.want) {
			t.Errorf("%q: part %d may run in %q, want %q", tt.cmd, tt.part, got, tt.want)
			continue
		}
		for _, w := range tt.want {
			if !slices.Contains(got, w) {
				t.Errorf("%q: part %d may run in %q, want %q", tt.cmd, tt.part, got, tt.want)
				break
			}
		}
	}
}

// knowledge/decisions/hook-decisions-v0.md: deny commands that write a
// protected path, wherever the command may run (GNU Bash manual §3.2.4,
// §4.1); output redirects of every form, >&word included (§3.6.4
// Redirecting Standard Output and Standard Error: unless word is a number
// or -, >&word sends both to the file word); and a file an option names
// for output: git-diff(1) and git-log(1) --output=<file>,
// git-format-patch(1) -o <dir>, and PowerShell's Invoke-WebRequest
// -OutFile.
func TestPreBashWritesWhereverTheyMayRun(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	out := filepath.ToSlash(scratch(t))
	bash := func(cmd string) hookio.Decision {
		return r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}})
	}
	pwsh := func(cmd string) hookio.Decision {
		return r.PreBash(hookio.Input{ToolName: "PowerShell", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}})
	}
	tests := []struct {
		run  func(string) hookio.Decision
		cmd  string
		deny bool
	}{
		{bash, "cd src; rm -rf ../snapshots", true},
		{bash, "cd src\nrm -rf ../snapshots", true},
		{bash, "cd src || exit 1; rm -rf ../snapshots", true},
		{bash, "cd src && true; rm ../snapshots/s.txt", true},
		{bash, "cd src/a; cd ..; rm -rf ../snapshots", true},
		{pwsh, `Set-Location src; Remove-Item -Recurse ..\snapshots`, true},
		{bash, "cd $D; rm -rf ../snapshots", true},
		{bash, "cd $D && rm -rf ../../snapshots", true},
		{bash, "popd; rm ../snapshots/s.txt", true},
		{bash, "cd src; rm -rf ../snapshots # tidy", true},
		{bash, "source env.sh; echo x > ../snapshots/s.txt", true},
		{bash, "cd src; ls ../snapshots", false},
		{bash, "cd $D && rm -rf build", false},
		{bash, "cd " + out + " && rm -rf snapshots", false},

		{bash, "echo x >& snapshots/s.txt", true},
		{bash, "echo x >&snapshots/s.txt", true},
		{bash, "cd src && go test >& ../snapshots/s.txt", true},
		{bash, "go test ./... >& out.txt", false},
		{bash, "go test ./... 2>&1 | tee out.txt", false},
		{bash, "echo x >&2", false},
		{bash, "exec 3>&-", false},

		{bash, "git diff --output=snapshots/s.txt", true},
		{bash, "git log -p --output snapshots/s.txt", true},
		{bash, "git -C . diff --output=snapshots/s.txt", true},
		{bash, "git format-patch -o snapshots HEAD~1", true},
		{pwsh, `Invoke-WebRequest https://example.com -OutFile snapshots\s.txt`, true},
		{bash, "git diff --output=out.patch", false},
		{bash, "git diff --stat -- snapshots", false},
		{bash, "grep -o snapshots README.md", false},
	}
	for _, tt := range tests {
		d := tt.run(tt.cmd)
		if (d.Deny != "") != tt.deny {
			t.Errorf("%q: deny=%q, want deny=%v", tt.cmd, d.Deny, tt.deny)
		}
	}
}
