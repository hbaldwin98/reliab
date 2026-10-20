package guard

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

// GNU Bash manual §3.5.2 Tilde Expansion: a tilde expands only where it
// begins a word, and PowerShell's ~ only as a path's first character, so a
// cd into a Windows short name such as C:\Users\RUNNER~1 is followed, as
// any literal directory is (knowledge/decisions/hook-decisions-v0.md).
func TestPartsTildeInsideAName(t *testing.T) {
	t.Parallel()
	session, out := scratch(t), scratch(t)
	dir := filepath.Join(out, "a~1")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ps := parts(session, "cd "+filepath.ToSlash(dir)+" && rm x", false)
	if got := ps[1].cwds[0]; got != dir {
		t.Errorf("cd into a~1: runs in %q, want %q", got, dir)
	}
	ps = parts(session, "cd ~1 && rm x", false)
	if got := ps[1].cwds; len(got) != 1 || got[0] != session {
		t.Errorf("cd ~1: runs in %q, want the session's", got)
	}
}

// knowledge/decisions/hook-decisions-v0.md: an option naming a file for
// output is a write target: git-diff(1) --output=<file> or --output <file>;
// Invoke-WebRequest -OutFile <path> or, as PowerShell binds any parameter,
// -OutFile:<path> (about_Parameters); git-format-patch(1) -o <dir>, the
// value attached or not (gitcli(7): a short option's value may be stuck);
// curl(1) -o. git-ls-files(1) -o is --others and names no output, and an
// option with no word after it names nothing.
func TestOutputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ws   []string
		want []string
	}{
		{[]string{"git", "diff", "--output=Snap/x"}, []string{"Snap/x"}},
		{[]string{"git", "diff", "--output", "Snap/x"}, []string{"Snap/x"}},
		{[]string{"Invoke-WebRequest", "u", "-OutFile:Snap\\x"}, []string{"Snap\\x"}},
		{[]string{"Invoke-WebRequest", "u", "-OutFile", "x"}, []string{"x"}},
		{[]string{"git", "format-patch", "-oDir"}, []string{"Dir"}},
		{[]string{"git", "format-patch", "-o", "Dir"}, []string{"Dir"}},
		{[]string{"curl", "-o", "x", "u"}, []string{"x"}},
		{[]string{"git", "ls-files", "-o", "x"}, nil},
		{[]string{"echo", "-oops", "x"}, nil},
		{[]string{"curl", "u", "-o"}, nil},
		{[]string{"git", "diff", "--output"}, nil},
	}
	for _, tt := range tests {
		if got := outputs(tt.ws); !slices.Equal(got, tt.want) {
			t.Errorf("outputs(%q) = %q, want %q", tt.ws, got, tt.want)
		}
	}
}

// knowledge/decisions/hook-decisions-v0.md: after a cd the hook cannot
// follow, a path is judged from every directory holding a file git sees;
// and env(1) -C moves its command into the directory it names, from
// wherever the command may run, so after cd src; the -C path resolves
// from src too.
func TestPreBashFromEveryDirectory(t *testing.T) {
	t.Parallel()
	r := open(t, "protected_paths: [\"src/a/oracle.txt\", \"snapshots/**\"]\ncommands:\n  build: \"true\"\n")
	bash := func(cmd string) hookio.Decision {
		return r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}})
	}
	for _, tt := range []struct {
		cmd  string
		deny bool
	}{
		{"cd $D && rm oracle.txt", true},
		{"cd $D && rm other.txt", false},
		{"cd src; env -C ../snapshots rm s.txt", true},
		{"env -C ../snapshots rm s.txt", false},
	} {
		if d := bash(tt.cmd); (d.Deny != "") != tt.deny {
			t.Errorf("%q: deny=%q, want deny=%v", tt.cmd, d.Deny, tt.deny)
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
		{bash, "git format-patch -osnapshots HEAD~1", true},
		{pwsh, `Invoke-WebRequest https://example.com -OutFile:snapshots\s.txt`, true},
		{bash, "curl -o snapshots/s.txt https://example.com", true},
		{bash, "sort -o snapshots/s.txt x", true},
		// git-ls-files(1) -o is --others, git-grep(1) -o --only-matching,
		// git-commit(1) -o --only: none names an output.
		{bash, "git ls-files -o snapshots", false},
		{bash, "git grep -o snapshots src", false},
		{bash, "git commit -o snapshots/s.txt -m wip", false},

		// A cd in a subshell or substitution moves what follows it there;
		// in PowerShell a group or script block keeps the location
		// (about_Operators: grouping operator; about_Script_Blocks).
		{bash, "(cd src; rm -rf ../snapshots)", true},
		{bash, "(cd src && rm -rf ../snapshots)", true},
		{bash, "x=$(cd src; rm -rf ../snapshots)", true},
		{bash, `echo "$(cd src && rm ../snapshots/s.txt)"`, true},
		{pwsh, `(cd src); Remove-Item -Recurse ../snapshots`, true},
		{pwsh, `(sl src); Remove-Item ../snapshots/s.txt`, true},
		{pwsh, `&{cd src}; Remove-Item ../snapshots/s.txt`, true},
		{bash, `git commit -m "relocation fix; rm ../snapshots/s.txt"`, false},
		// Above the root: the repository's own name leads back into it.
		{bash, "cd $D; rm -rf " + filepath.Base(r.Root) + "/snapshots", true},

		// PowerShell's built-in aliases (about_Aliases; Get-Alias): del,
		// erase, ri, rd for Remove-Item; copy, cpi; move, mi; sc, ac; ni;
		// ren, rni; clc.
		{pwsh, `del snapshots\s.txt`, true},
		{pwsh, `Set-Location src; del ..\snapshots\s.txt`, true},
		{pwsh, `ri -Recurse snapshots`, true},
		{pwsh, `rd snapshots`, true},
		{pwsh, `copy x snapshots\s.txt`, true},
		{pwsh, `sc snapshots\s.txt x`, true},
	}
	if runtime.GOOS == "windows" {
		// Git Bash on Windows names drive C: /c (cdTarget reads it so).
		gb := "/" + strings.ToLower(r.Root[:1]) + filepath.ToSlash(r.Root[2:])
		tests = append(tests, struct {
			run  func(string) hookio.Decision
			cmd  string
			deny bool
		}{bash, "rm " + gb + "/snapshots/s.txt", true})
		// A drive's root holds no repository's file; the hook judges it
		// rather than failing.
		tests = append(tests, struct {
			run  func(string) hookio.Decision
			cmd  string
			deny bool
		}{bash, "cp x /c", false})
	}
	for _, tt := range tests {
		d := tt.run(tt.cmd)
		if (d.Deny != "") != tt.deny {
			t.Errorf("%q: deny=%q, want deny=%v", tt.cmd, d.Deny, tt.deny)
		}
	}
}
