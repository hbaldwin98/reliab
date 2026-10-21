package guard

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
)

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
