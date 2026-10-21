package effects

import (
	"slices"
	"strings"
	"testing"
)

// all returns every value of one effect across a line's commands, sorted.
func all(e *Effects, f func(Command) []string) []string {
	var out []string
	for _, c := range e.Commands {
		out = append(out, f(c)...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func writes(c Command) []string { return c.Writes }
func whole(c Command) []string  { return c.Whole }

// knowledge/decisions/hook-decisions-v0.md lists what a line may write:
// redirects (>, >>, 2>, &>, >&file, >|; GNU Bash manual §3.6), a write
// verb's operands in its own simple command (sed -i, cp, mv, rm, tee, git
// checkout|restore, Set-Content, Remove-Item and PowerShell's aliases), and
// a file an output option names. knowledge/decisions/protected-path-
// enforcement.md §7: only rm, rmdir, git checkout|restore|rm|clean|reset,
// and Remove-Item (and aliases) rewrite or remove a directory whole.
// knowledge/decisions/command-effects.md adds find -delete and find -exec
// with a write verb, dd of=, and a write verb's path behind a wrapper.
func TestWritesAndWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line  string
		sh    Shell
		write []string
		whole []string
	}{
		{"echo x > a 2> b &> c", Bash, []string{"a", "b", "c"}, nil},
		{"echo x >> a >| b >&c", Bash, []string{"a", "b", "c"}, nil},
		{"go test ./... 2>&1 >&2 3>&-", Bash, nil, nil},
		{"cat < in > out", Bash, []string{"out"}, nil},
		{"cp a b", Bash, []string{"a", "b"}, nil},
		{"rm -rf d", Bash, []string{"-rf", "d"}, []string{"-rf", "d"}},
		{"/bin/rm d", Bash, []string{"d"}, []string{"d"}},
		{"sudo -u x rm d", Bash, []string{"d"}, []string{"d"}},
		{"xargs -I{} rm {}", Bash, []string{"{}"}, []string{"{}"}},
		{"timeout 5 mv a b", Bash, []string{"a", "b"}, nil},
		{"sed 's/a/b/' f", Bash, nil, nil},
		{"sed -n -e p -i f", Bash, []string{"-e", "-i", "-n", "f", "p"}, nil},
		{"perl -pi -e s/a/b/ f", Bash, []string{"-e", "-pi", "f", "s/a/b/"}, nil},
		{"git -C x checkout -- f", Bash, []string{"--", "f"}, []string{"--", "f"}},
		{"git mv a b", Bash, []string{"a", "b"}, nil},
		{"git log -- f", Bash, nil, nil},
		// git-restore(1): --staged restores only the index unless
		// --worktree is given too; git-reset(1) touches files only with
		// --hard, --merge, or --keep.
		{"git restore --staged f", Bash, nil, nil},
		{"git restore -S .", Bash, nil, nil},
		{"git restore --staged --worktree f", Bash, []string{"--staged", "--worktree", "f"}, []string{"--staged", "--worktree", "f"}},
		{"git restore -SW f", Bash, []string{"-SW", "f"}, []string{"-SW", "f"}},
		{"git restore f", Bash, []string{"f"}, []string{"f"}},
		{"git reset HEAD -- f", Bash, nil, nil},
		{"git reset --soft HEAD~1", Bash, nil, nil},
		{"git reset --hard HEAD", Bash, []string{"--hard", "HEAD"}, []string{"--hard", "HEAD"}},
		{"git reset --keep main", Bash, []string{"--keep", "main"}, []string{"--keep", "main"}},
		{"dd if=a of=b", Bash, []string{"b"}, nil},
		{"find d -name x -delete", Bash, []string{"d"}, []string{"d"}},
		{"find d -exec rm {} +", Bash, []string{"d", "{}"}, []string{"d", "{}"}},
		{"find d -name x -print", Bash, nil, nil},
		// A write verb is the command run, not any word: these name rm as
		// data.
		{"grep -rn cp snapshots", Bash, nil, nil},
		{"echo rm snapshots", Bash, nil, nil},
		{"go install ./...", Bash, nil, nil},
		{"Remove-Item -Recurse d", PowerShell, []string{"-Recurse", "d"}, []string{"-Recurse", "d"}},
		{"Set-Content -Path:f x", PowerShell, []string{"-Path:f", "f", "x"}, nil},
		{"copy a b", PowerShell, []string{"a", "b"}, nil},
		{"echo x *> f", PowerShell, []string{"f"}, nil},
		// Quoted text is a word, never a redirect or a verb.
		{`git commit -m "fix > snapshots/s.txt; rm x"`, Bash, nil, nil},
	}
	for _, tt := range tests {
		e := Read(tt.line, tt.sh, t.TempDir())
		if got := all(e, writes); !slices.Equal(got, tt.write) {
			t.Errorf("%q writes %q, want %q", tt.line, got, tt.write)
		}
		if got := all(e, whole); !slices.Equal(got, tt.whole) {
			t.Errorf("%q removes whole %q, want %q", tt.line, got, tt.whole)
		}
	}
}

// Code a command runs is read as a line of its own (knowledge/decisions/
// command-effects.md): bash(1) -c, pwsh -Command, cmd /c, eval,
// Invoke-Expression, git-rebase(1) --exec, a git alias starting with !
// (git-config(1) alias.*), command and process substitution (GNU Bash
// manual §3.5.4, §3.5.6), a subshell, a heredoc or here-string fed to a
// shell (§3.6.6, §3.6.7), and text piped into one. A heredoc fed to any
// other command is its data.
func TestNestedCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line  string
		sh    Shell
		write []string
	}{
		{"bash -c 'cp x f'", Bash, []string{"f", "x"}},
		{"sh -lc 'rm f'", Bash, []string{"f"}},
		{"sudo bash -c 'rm f'", Bash, []string{"f"}},
		{`pwsh -Command "Remove-Item f"`, Bash, []string{"f"}},
		{`cmd /c "del f"`, PowerShell, []string{"f"}},
		{"eval 'rm f'", Bash, []string{"f"}},
		{`iex "Remove-Item f"`, PowerShell, []string{"f"}},
		{`git rebase --exec "rm f" main`, Bash, []string{"f"}},
		{`git -c alias.x='!rm f' x`, Bash, []string{"f"}},
		{"x=$(rm f)", Bash, []string{"f"}},
		{"echo `rm f`", Bash, []string{"f"}},
		{"cat <(rm f)", Bash, []string{"f"}},
		{"(rm f)", Bash, []string{"f"}},
		{`echo (Remove-Item f)`, PowerShell, []string{"f"}},
		{"&{Remove-Item f}", PowerShell, []string{"f"}},
		{"sh <<EOF\nrm f\nEOF", Bash, []string{"f"}},
		{"bash <<< 'rm f'", Bash, []string{"f"}},
		{"cat <<'EOF' | sh\nrm f\nEOF", Bash, []string{"f"}},
		{"echo 'rm f' | bash", Bash, []string{"f"}},
		{"cat > notes <<'EOF'\nrm f\nEOF", Bash, []string{"notes"}},
		{"python - <<'EOF'\nrm f\nEOF", Bash, nil},
		{"echo 'rm f' | grep rm", Bash, nil},
	}
	for _, tt := range tests {
		if got := all(Read(tt.line, tt.sh, t.TempDir()), writes); !slices.Equal(got, tt.write) {
			t.Errorf("%q writes %q, want %q", tt.line, got, tt.write)
		}
	}
}

// knowledge/decisions/pre-push-v0.md gates a push or PR creation: git push
// past git's global options (git(1) SYNOPSIS), and gh pr create, where a
// shell runs them; not their words in data.
func TestPush(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line string
		sh   Shell
		push bool
	}{
		{"git push", Bash, true},
		{"git -C x -c a=b --no-pager push origin main", Bash, true},
		{"git status && git push", Bash, true},
		{"gh pr create --fill", Bash, true},
		{`bash -c "git push origin main"`, Bash, true},
		{"git push", PowerShell, true},
		{"git log --grep push", Bash, false},
		{`git commit -m "git push later"`, Bash, false},
		{`go test -args '{"command":"git push"}'`, Bash, false},
		{`echo '{"command":"git push"}' | reliab hook pre-bash`, Bash, false},
		{"gh pr view", Bash, false},
	}
	for _, tt := range tests {
		if got := Read(tt.line, tt.sh, t.TempDir()).Push; got != tt.push {
			t.Errorf("%q: push=%v, want %v", tt.line, got, tt.push)
		}
	}
}

// knowledge/decisions/hook-decisions-v0.md: the person-only commands match
// only text Bash may run. A line all of whose commands print, record,
// search, or change directory has its data blanked in Code and its data
// words never invoke reliab; anything doubtful judges the line whole.
func TestData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line    string
		sh      Shell
		invokes bool
		keep    string // how Code starts when the line is all data
	}{
		{`git commit -m "a person runs reliab approve x"`, Bash, false, "git commit "},
		{"cd d && echo 'reliab approve x'", Bash, false, "cd   && echo "},
		{"git -C d log --grep 'reliab approve'", Bash, false, "git -C d log "},
		{"git commit -m \"$(cat <<'EOF'\nreliab approve x\nEOF\n)\"", Bash, false, "git commit "},
		{`git commit -m "$(reliab approve x)"`, Bash, true, ""},
		{"echo 'reliab approve x' | sh", Bash, true, ""},
		{"echo 'reliab approve x' > r.sh", Bash, true, ""},
		{"echo 'reliab approve x'; git", Bash, true, ""},
		{"git -c core.pager=x log 'reliab approve'", Bash, true, ""},
		{`git commit -m "reliab approve x"`, PowerShell, true, ""},
		{`echo "reliab approve x"`, Unknown, true, ""},
	}
	for _, tt := range tests {
		e := Read(tt.line, tt.sh, t.TempDir())
		if got := e.Invokes("approve"); got != tt.invokes {
			t.Errorf("%q: invokes approve=%v, want %v", tt.line, got, tt.invokes)
		}
		if tt.keep == "" {
			if e.Code != tt.line {
				t.Errorf("%q is not all data, but its code is %q", tt.line, e.Code)
			}
			continue
		}
		// Each byte stays in place and each newline stays; the data goes.
		if len(e.Code) != len(tt.line) || strings.Count(e.Code, "\n") != strings.Count(tt.line, "\n") ||
			!strings.HasPrefix(e.Code, tt.keep) || strings.Contains(e.Code, "reliab") {
			t.Errorf("%q: code %q, want it to start %q with its data blanked", tt.line, e.Code, tt.keep)
		}
	}
}

// PowerShell reads curly quotes as quotes, a no-break space as a space, a
// dash as a hyphen before a parameter, and a backtick as its escape
// (about_Quoting_Rules, about_Special_Characters, about_Parsing); Bash
// reads each as plain text.
func TestPowerShellWords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line string
		sh   Shell
		want []string
	}{
		{"Copy-Item “a b” ‘c’", PowerShell, []string{"Copy-Item", "a b", "c"}},
		{"Copy-Item a", PowerShell, []string{"Copy-Item", "a"}},
		{"Remove-Item –Recurse d", PowerShell, []string{"Remove-Item", "-Recurse", "d"}},
		{"reli`ab 'it''s'", PowerShell, []string{"reliab", "it's"}},
		{"echo “a b”", Bash, []string{"echo", "“a", "b”"}},
		{"echo a b", Bash, []string{"echo", "a b"}},
	}
	for _, tt := range tests {
		if got := Read(tt.line, tt.sh, t.TempDir()).Commands[0].Words; !slices.Equal(got, tt.want) {
			t.Errorf("%q: words %q, want %q", tt.line, got, tt.want)
		}
	}
}
