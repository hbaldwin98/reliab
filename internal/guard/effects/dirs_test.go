package effects

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// knowledge/decisions/hook-decisions-v0.md: a relative path counts where its
// simple command runs, and the hook never judges a path outside the
// repository unless the line surely runs it there. A session was denied
// `cd <scratch repo> && git checkout -q -- .`, because . was read as this
// repository's root. These tests moved here from the guard's parts tests;
// each row now names one directory per simple command the line runs, nested
// ones included, so a redirect's & (2>&1) no longer splits a command, and a
// comment's text runs nothing.

// scratch makes a directory outside any repository, with a subdirectory sub
// and one whose name has a space.
func scratch(t *testing.T) string {
	t.Helper()
	// Resolved, so a cd into it has one directory both as named and as
	// symlinks lead: macOS's temporary directory is under /var, a link to
	// /private/var.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"sub", "with space"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func bashOrPwsh(pwsh bool) Shell {
	if pwsh {
		return PowerShell
	}
	return Bash
}

// firstDirs returns, for each command a line runs, the directory it runs in
// as far as the hook can follow (Command.Dirs[0]).
func firstDirs(e *Effects) []string {
	var out []string
	for _, c := range e.Commands {
		out = append(out, c.Dirs[0])
	}
	return out
}

func TestDirsWhereEachCommandRuns(t *testing.T) {
	t.Parallel()
	session, out := scratch(t), scratch(t)
	// Literal directories named as the shell would expand them: a cd to one
	// must still not be followed.
	for _, d := range []string{"$X", "~", "[a]", "%X%"} {
		if err := os.MkdirAll(filepath.Join(session, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	o := filepath.ToSlash(out)
	sub, space := filepath.Join(out, "sub"), filepath.Join(out, "with space")
	S := session
	tests := []struct {
		cmd  string
		pwsh bool
		want []string // the directory each command runs in, as named
	}{
		{"git checkout -q -- .", false, []string{S}},
		{"cd " + o + " && git checkout -q -- .", false, []string{S, out}},
		{"cd " + o + " && cd sub && ls && rm x", false, []string{S, out, sub, sub}},
		{"cd " + o + "/sub && cd .. && rm x", false, []string{S, sub, out}},
		{`cd "` + o + `/with space" && rm x`, false, []string{S, space}},
		{"cd '" + o + "'/sub && rm x", false, []string{S, sub}},
		{"cd -L -- " + o + " && rm x", false, []string{S, out}},
		{"X=1 cd " + o + " && rm x", false, []string{S, out}},
		{"time cd " + o + " && rm x", false, []string{S, out}},
		{"if cd " + o + " && rm x; then echo; fi", false, []string{S, out, S, S}},
		{"echo; then cd " + o + " && rm x", false, []string{S, S, out}},
		{"a || b && cd " + o + " && rm x", false, []string{S, S, S, out}},
		{"cd " + o + " && a | b && rm x", false, []string{S, out, out, out}},
		{"a & cd " + o + " && rm x", false, []string{S, S, out}},
		// A redirect's & does not split the simple command.
		{"cd " + o + " && go test 2>&1 && rm x", false, []string{S, out, out}},
		{"cd " + o + " && go test &>/dev/null && rm x", false, []string{S, out, out}},
		{`Set-Location -Path "` + out + `" && Remove-Item x`, true, []string{S, out}},
		{`sl -LiteralPath ` + out + ` && Remove-Item x`, true, []string{S, out}},

		// Only && carries a cd, and only one that surely ran.
		{"cd " + o + "; rm x", false, []string{S, S}},
		{"cd " + o + "\nrm x", false, []string{S, S}},
		{"cd " + o + " &&\nrm x", false, []string{S, S}},
		{"cd " + o + " || rm x", false, []string{S, S}},
		{"cd " + o + " | rm x", false, []string{S, S}},
		{"cd " + o + " & rm x", false, []string{S, S}},
		{"cd " + o + " && a || rm x", false, []string{S, out, S}},
		{"cd " + o + " && a || cd sub && rm x", false, []string{S, out, S, S}},
		{"true || cd " + o + " && rm x", false, []string{S, S, S}},
		{"true || cd " + o + "; rm x", false, []string{S, S, S}},
		{"false && cd " + o + "; rm x", false, []string{S, S, S}},
		{"cd " + o + " && rm x; rm y", false, []string{S, out, S}},
		// A cd the hook cannot follow.
		{"cd $D && rm x", false, []string{S, S}},
		{"cd - && rm x", false, []string{S, S}},
		{"cd && rm x", false, []string{S, S}},
		{"cd ~ && rm x", false, []string{S, S}},
		{"cd $X && rm x", false, []string{S, S}},
		{"cd [a] && rm x", false, []string{S, S}},
		{"cd %X% && rm x", false, []string{S, S}},
		// Rooted with no drive on Windows; missing on other systems.
		{"cd /sub && rm x", false, []string{S, S}},
		{"cd $D && cd sub && rm x", false, []string{S, S, S}},
		{"cd " + o + " && popd && cd sub && rm x", false, []string{S, out, S, S}},
		{"cd " + o + "/s* && rm x", false, []string{S, S}},
		{"cd " + o + "/missing && rm x", false, []string{S, S}},
		{"cd " + o + " sub && rm x", false, []string{S, S}},
		{"cd -P " + o + " && rm x", false, []string{S, S}},
		{"pushd -n " + o + " && rm x", false, []string{S, S}},
		{"pushd +1 && rm x", false, []string{S, S}},
		{"cd " + o + " >/dev/null && rm x", false, []string{S, S}},
		{`cd C:\x && rm x`, false, []string{S, S}},
		{`Set-Location -StackName ` + out + ` && Remove-Item x`, true, []string{S, S}},
		{`Set-Location -Path:` + out + ` && Remove-Item x`, true, []string{S, S}},
		// Something after a cd may move the directory again.
		{"cd " + o + " && popd && rm x", false, []string{S, out, S}},
		{"cd " + o + " && ! cd sub && rm x", false, []string{S, out, S}},
		{"cd " + o + " && $C sub && rm x", false, []string{S, out, S}},
		{"cd " + o + " && echo cd && rm x", false, []string{S, out, S}},
		{"Set-Location " + out + " && cd.. && Remove-Item x", true, []string{S, out, S}},
		// The whole line is doubtful. Code nested in a command (eval's
		// argument, a subshell, a substitution) is a command too.
		{"cd " + o + " && command cd sub && rm x", false, []string{S, S, S}},
		{"cd " + o + " && builtin cd sub && rm x", false, []string{S, S, S}},
		{"cd " + o + " && eval cd sub && rm x", false, []string{S, S, S, S}},
		{"cd " + o + " && . ./env.sh && rm x", false, []string{S, S, S}},
		{"cd " + o + " && exec rm x", false, []string{S, S}},
		{"alias cd=true\ncd " + o + " && rm x", false, []string{S, S, S}},
		{"for i in 1 2; do cd .. && rm x; done", false, []string{S, S, S, S}},
		{"cd " + o + " && (cd sub) && rm x", false, []string{S, S, S}},
		{"cd " + o + " && { cd sub; } && rm x", false, []string{S, S, S, S}},
		{"cd " + o + " && echo `cd sub` && rm x", false, []string{S, S, S, S}},
		{"cd " + o + " && cat <<EOF && rm x\nEOF", false, []string{S, S, S}},
		{"cd " + o + " && echo # && rm x", false, []string{S, S}},
		{`echo x\;cd ` + o + ` && rm x`, false, []string{S, S}},
		{`echo "a;cd ` + o + `" && rm x`, false, []string{S, S}},
		{`echo 'a; cd ` + o + `' && rm x`, false, []string{S, S}},
		{`echo \'; cd ` + o + ` && rm x`, false, []string{S, S, S}},
		// Bash reads a lone carriage return as text; the line is also read
		// with it as a line break, whose commands follow the first two.
		{"cd " + o + " && echo hi\rrm x", false, []string{S, S, S, out, S}},
		{"echo \u2018; Set-Location " + out + " && \u2019 && Remove-Item x", true, []string{S, S}},
		{"echo --% ; cd " + out + " && x | Remove-Item x", true, []string{S, S, S, S}},
		{"CDPATH=" + o + " cd sub && rm x", false, []string{S, S}},
		{"Set-Location " + out + " && .\\build.ps1 && Remove-Item x", true, []string{S, S, S}},
	}
	for _, tt := range tests {
		got := firstDirs(Read(tt.cmd, bashOrPwsh(tt.pwsh), session))
		if !slices.Equal(got, tt.want) {
			t.Errorf("%q: commands run in %q, want %q", tt.cmd, got, tt.want)
		}
	}
}

// A cd resolves logically, as Bash and PowerShell do, and the commands after
// it are judged both there and where symlinks lead.
func TestDirsThroughSymlink(t *testing.T) {
	t.Parallel()
	session, out := t.TempDir(), scratch(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Join(out, "sub"), link); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	e := Read("cd "+filepath.ToSlash(link)+" && rm ../x", Bash, session)
	real, _ := filepath.EvalSymlinks(filepath.Join(out, "sub"))
	if got := e.Commands[1].Dirs; len(got) != 2 || got[0] != link || got[1] != real {
		t.Errorf("dirs = %q, want [%q %q]", got, link, real)
	}
	e = Read("cd "+filepath.ToSlash(link)+"/.. && rm x", Bash, session)
	if got := e.Commands[1].Dirs[0]; got != filepath.Dir(link) {
		t.Errorf("cd link/.. went to %q, want the logical parent %q", got, filepath.Dir(link))
	}
}

// The test name must not hold "cdpath": it is in the temp directory's path.
func TestDirsBareCdSearchPath(t *testing.T) {
	session, out := t.TempDir(), scratch(t)
	o := filepath.ToSlash(out)
	t.Setenv("CDPATH", session)
	e := Read("cd "+o+" && cd sub && rm x", Bash, session)
	if got := e.Commands[2].Dirs[0]; got != session {
		t.Errorf("a bare cd under CDPATH: runs in %q, want the session's", got)
	}
	e = Read("cd "+o+" && cd ./sub && rm x", Bash, session)
	if got := e.Commands[2].Dirs[0]; got != filepath.Join(out, "sub") {
		t.Errorf("cd ./sub under CDPATH went to %q", got)
	}
}

func TestDirsCannotEnter(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	session, out := t.TempDir(), scratch(t)
	locked := filepath.Join(out, "sub")
	if err := os.Chmod(locked, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if got := Read("cd "+locked+" && rm x", Bash, session).Commands[1].Dirs[0]; got != session {
		t.Errorf("a cd into a directory with no search permission went to %q", got)
	}
}

func TestDirsGitBashDrive(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("Git Bash drive paths exist only on Windows")
	}
	session, out := t.TempDir(), scratch(t)
	vol := filepath.VolumeName(out)
	msys := "/" + strings.ToLower(vol[:1]) + filepath.ToSlash(out[len(vol):])
	if got := Read("cd "+msys+" && rm x", Bash, session).Commands[1].Dirs[0]; !strings.EqualFold(got, out) {
		t.Errorf("cd %s went to %q, want %q", msys, got, out)
	}
	// PowerShell reads /c as a folder at the current drive's root.
	if got := Read("cd "+msys+" && Remove-Item x", PowerShell, session).Commands[1].Dirs[0]; got != session {
		t.Errorf("PowerShell cd %s went to %q, want the session's", msys, got)
	}
	if got := Read("cd "+filepath.ToSlash(out[len(vol):])+" && rm x", Bash, session).Commands[1].Dirs[0]; got != session {
		t.Errorf("a rooted path with no drive went to %q, want the session's", got)
	}
	// /c alone is the drive's root.
	drive := "/" + strings.ToLower(vol[:1])
	if got := Read("cd "+drive+" && rm x", Bash, session).Commands[1].Dirs[0]; !strings.EqualFold(got, vol+`\`) {
		t.Errorf("cd %s went to %q, want %q", drive, got, vol+`\`)
	}
}

// An open double quote that ends in a backslash is still read, not a crash:
// a hook that fails lets the command through.
func TestWordsUnclosedQuoteEndingInBackslash(t *testing.T) {
	t.Parallel()
	if got := Read(`echo "a\`, Bash, t.TempDir()).Commands[0].Words; len(got) != 2 || got[0] != "echo" {
		t.Errorf("words = %q", got)
	}
}

// GNU Bash manual §3.1.2.3 Double Quotes: inside double quotes a backslash
// followed by a backslash is one backslash. So `cd "<dir>\\sub"` names
// `<dir>\sub`, a path with a backslash in it, which the hook does not follow
// (fail closed, per the rule at the top of cwd.go), and the reader reads the
// pair once. A reader that re-read the pair would never return, so the call
// runs under a deadline, and a miss ends the test binary instead of leaving
// it to spin.
func TestWordsEscapedBackslashInDoubleQuotes(t *testing.T) {
	t.Parallel()
	session, out := t.TempDir(), scratch(t)
	o := filepath.ToSlash(out)
	type result struct{ words, cd *Effects }
	done := make(chan result, 1)
	go func() {
		done <- result{Read(`echo "a\\b" c`, Bash, session), Read(`cd "`+o+`\\sub" && rm x`, Bash, session)}
	}()
	var r result
	select {
	case r = <-done:
	case <-time.After(2 * time.Second):
		panic(`the reader did not return on an escaped backslash inside double quotes`)
	}
	if want := []string{"echo", `a\b`, "c"}; !slices.Equal(r.words.Commands[0].Words, want) {
		t.Errorf("words = %q, want %q", r.words.Commands[0].Words, want)
	}
	if len(r.cd.Commands) != 2 || r.cd.Commands[1].Dirs[0] != session {
		t.Errorf("a cd to a quoted path with a backslash was followed: %+v", r.cd.Commands)
	}
}

func TestDirsDirFlags(t *testing.T) {
	t.Parallel()
	session, out := t.TempDir(), scratch(t)
	o := filepath.ToSlash(out)
	e := Read("cd "+o+" && env -C "+o+"/sub rm x", Bash, session)
	want := []string{out, session, filepath.Join(out, "sub")}
	if got := e.Commands[1].Dirs; !slices.Equal(got, want) {
		t.Errorf("env -C: dirs = %q, want %q", got, want)
	}
	e = Read("make --directory=sub install", Bash, session)
	want = []string{session, session, filepath.Join(session, "sub")}
	if got := e.Commands[0].Dirs; !slices.Equal(got, want) {
		t.Errorf("--directory=: dirs = %q, want %q", got, want)
	}
}

// GNU Bash manual §3.2.4 Lists: commands separated by ; run in sequence,
// and after || the next runs when the one before failed; §4.1 cd changes
// the shell's working directory, which no separator undoes. So a cd stays
// in effect after ;, ||, and a newline, and one the hook cannot follow may
// lead anywhere (knowledge/decisions/hook-decisions-v0.md: a write to a
// protected path is denied wherever its simple command may run). A review
// of v0.1.3 allowed cd internal; rm ../reliability-profile.yaml.
func TestDirsMayRun(t *testing.T) {
	t.Parallel()
	session, out := scratch(t), scratch(t)
	o := filepath.ToSlash(out)
	tests := []struct {
		cmd  string
		cmdN int
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
		{"cd $D && rm x", 1, []string{session, Anywhere}},
		{"cd $D; cd " + o + " && rm x", 2, []string{out, Anywhere}},
		{"popd; rm x", 1, []string{session, Anywhere}},
		// A line the hook cannot track: anywhere when it may change directory.
		{"cd " + o + "; rm x # tidy", 1, []string{session, Anywhere}},
		{"source env.sh; rm x", 1, []string{session, Anywhere}},
		// The substitution's date is a command of its own, before echo's.
		{"echo $(date); rm x", 2, []string{session}},
	}
	for _, tt := range tests {
		e := Read(tt.cmd, Bash, session)
		if tt.cmdN >= len(e.Commands) {
			t.Errorf("%q: %d commands, want more than %d", tt.cmd, len(e.Commands), tt.cmdN)
			continue
		}
		got := slices.Clone(e.Commands[tt.cmdN].Dirs)
		want := slices.Clone(tt.want)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%q: command %d may run in %q, want %q", tt.cmd, tt.cmdN, e.Commands[tt.cmdN].Dirs, tt.want)
		}
	}
}

// GNU Bash manual §3.5.2 Tilde Expansion: a tilde expands only where it
// begins a word, and PowerShell's ~ only as a path's first character, so a
// cd into a Windows short name such as C:\Users\RUNNER~1 is followed, as
// any literal directory is (knowledge/decisions/hook-decisions-v0.md).
func TestDirsTildeInsideAName(t *testing.T) {
	t.Parallel()
	session, out := scratch(t), scratch(t)
	dir := filepath.Join(out, "a~1")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Read("cd "+filepath.ToSlash(dir)+" && rm x", Bash, session).Commands[1].Dirs[0]; got != dir {
		t.Errorf("cd into a~1: runs in %q, want %q", got, dir)
	}
	if got := Read("cd ~1 && rm x", Bash, session).Commands[1].Dirs[0]; got != session {
		t.Errorf("cd ~1: runs in %q, want the session's", got)
	}
}
