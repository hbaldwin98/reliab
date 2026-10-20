package guard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/hookio"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// knowledge/decisions/hook-decisions-v0.md: a relative path counts where its
// simple command runs, and the hook never judges a path outside the
// repository unless the line surely runs it there. A session was denied
// `cd <scratch repo> && git checkout -q -- .`, because . was read as this
// repository's root.

// scratch makes a directory outside any repository, with a subdirectory sub
// and one whose name has a space.
func scratch(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"sub", "with space"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParts(t *testing.T) {
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
	tests := []struct {
		cmd  string
		pwsh bool
		want []string // the directory each part runs in, as named
	}{
		{"git checkout -q -- .", false, []string{session}},
		{"cd " + o + " && git checkout -q -- .", false, []string{session, out}},
		{"cd " + o + " && cd sub && ls && rm x", false, []string{session, out, sub, sub}},
		{"cd " + o + "/sub && cd .. && rm x", false, []string{session, sub, out}},
		{`cd "` + o + `/with space" && rm x`, false, []string{session, space}},
		{"cd '" + o + "'/sub && rm x", false, []string{session, sub}},
		{"cd -L -- " + o + " && rm x", false, []string{session, out}},
		{"X=1 cd " + o + " && rm x", false, []string{session, out}},
		{"time cd " + o + " && rm x", false, []string{session, out}},
		{"if cd " + o + " && rm x; then echo; fi", false, []string{session, out, session, session}},
		{"echo; then cd " + o + " && rm x", false, []string{session, session, out}},
		{"a || b && cd " + o + " && rm x", false, []string{session, session, session, out}},
		{"cd " + o + " && a | b && rm x", false, []string{session, out, out, out}},
		{"a & cd " + o + " && rm x", false, []string{session, session, out}},
		// A redirect's & does not split the simple command.
		{"cd " + o + " && go test 2>&1 && rm x", false, []string{session, out, out, out}},
		{"cd " + o + " && go test &>/dev/null && rm x", false, []string{session, out, out, out}},
		{`Set-Location -Path "` + out + `" && Remove-Item x`, true, []string{session, out}},
		{`sl -LiteralPath ` + out + ` && Remove-Item x`, true, []string{session, out}},

		// Only && carries a cd, and only one that surely ran.
		{"cd " + o + "; rm x", false, []string{session, session}},
		{"cd " + o + "\nrm x", false, []string{session, session}},
		{"cd " + o + " &&\nrm x", false, []string{session, session}},
		{"cd " + o + " || rm x", false, []string{session, session}},
		{"cd " + o + " | rm x", false, []string{session, session}},
		{"cd " + o + " & rm x", false, []string{session, session}},
		{"cd " + o + " && a || rm x", false, []string{session, out, session}},
		{"cd " + o + " && a || cd sub && rm x", false, []string{session, out, session, session}},
		{"true || cd " + o + " && rm x", false, []string{session, session, session}},
		{"true || cd " + o + "; rm x", false, []string{session, session, session}},
		{"false && cd " + o + "; rm x", false, []string{session, session, session}},
		{"cd " + o + " && rm x; rm y", false, []string{session, out, session}},
		// A cd the hook cannot follow.
		{"cd $D && rm x", false, []string{session, session}},
		{"cd - && rm x", false, []string{session, session}},
		{"cd && rm x", false, []string{session, session}},
		{"cd ~ && rm x", false, []string{session, session}},
		{"cd $X && rm x", false, []string{session, session}},
		{"cd [a] && rm x", false, []string{session, session}},
		{"cd %X% && rm x", false, []string{session, session}},
		// Rooted with no drive on Windows; missing on other systems.
		{"cd /sub && rm x", false, []string{session, session}},
		{"cd $D && cd sub && rm x", false, []string{session, session, session}},
		{"cd " + o + " && popd && cd sub && rm x", false, []string{session, out, session, session}},
		{"cd " + o + "/s* && rm x", false, []string{session, session}},
		{"cd " + o + "/missing && rm x", false, []string{session, session}},
		{"cd " + o + " sub && rm x", false, []string{session, session}},
		{"cd -P " + o + " && rm x", false, []string{session, session}},
		{"pushd -n " + o + " && rm x", false, []string{session, session}},
		{"pushd +1 && rm x", false, []string{session, session}},
		{"cd " + o + " >/dev/null && rm x", false, []string{session, session}},
		{`cd C:\x && rm x`, false, []string{session, session}},
		{`Set-Location -StackName ` + out + ` && Remove-Item x`, true, []string{session, session}},
		{`Set-Location -Path:` + out + ` && Remove-Item x`, true, []string{session, session}},
		// Something after a cd may move the directory again.
		{"cd " + o + " && popd && rm x", false, []string{session, out, session}},
		{"cd " + o + " && ! cd sub && rm x", false, []string{session, out, session}},
		{"cd " + o + " && $C sub && rm x", false, []string{session, out, session}},
		{"cd " + o + " && echo cd && rm x", false, []string{session, out, session}},
		{"Set-Location " + out + " && cd.. && Remove-Item x", true, []string{session, out, session}},
		// The whole line is doubtful.
		{"cd " + o + " && command cd sub && rm x", false, []string{session, session, session}},
		{"cd " + o + " && builtin cd sub && rm x", false, []string{session, session, session}},
		{"cd " + o + " && eval cd sub && rm x", false, []string{session, session, session}},
		{"cd " + o + " && . ./env.sh && rm x", false, []string{session, session, session}},
		{"cd " + o + " && exec rm x", false, []string{session, session}},
		{"alias cd=true\ncd " + o + " && rm x", false, []string{session, session, session}},
		{"for i in 1 2; do cd .. && rm x; done", false, []string{session, session, session, session}},
		{"cd " + o + " && (cd sub) && rm x", false, []string{session, session, session}},
		{"cd " + o + " && { cd sub; } && rm x", false, []string{session, session, session, session}},
		{"cd " + o + " && echo `cd sub` && rm x", false, []string{session, session, session}},
		{"cd " + o + " && cat <<EOF && rm x\nEOF", false, []string{session, session, session, session}},
		{"cd " + o + " && echo # && rm x", false, []string{session, session, session}},
		{`echo x\;cd ` + o + ` && rm x`, false, []string{session, session, session}},
		{`echo "a;cd ` + o + `" && rm x`, false, []string{session, session, session}},
		{`echo 'a; cd ` + o + `' && rm x`, false, []string{session, session, session}},
		{`echo \'; cd ` + o + ` && rm x`, false, []string{session, session, session}},
		{"cd " + o + " && echo hi\rrm x", false, []string{session, session}},
		{"echo \u2018; Set-Location " + out + " && \u2019 && Remove-Item x", true, []string{session, session, session, session}},
		{"echo --% ; cd " + out + " && x | Remove-Item x", true, []string{session, session, session, session}},
		{"CDPATH=" + o + " cd sub && rm x", false, []string{session, session}},
		{"Set-Location " + out + " && .\\build.ps1 && Remove-Item x", true, []string{session, session, session}},
	}
	for _, tt := range tests {
		ps := parts(session, tt.cmd, tt.pwsh)
		if len(ps) != len(tt.want) {
			t.Errorf("%q: %d parts, want %d", tt.cmd, len(ps), len(tt.want))
			continue
		}
		for i, p := range ps {
			if p.cwds[0] != tt.want[i] || tt.want[i] == session && len(p.cwds) != 1 {
				t.Errorf("%q: part %d %q runs in %q, want %q", tt.cmd, i, p.text, p.cwds, tt.want[i])
			}
		}
	}
}

// A cd resolves logically, as Bash and PowerShell do, and the commands after
// it are judged both there and where symlinks lead.
func TestPartsSymlink(t *testing.T) {
	t.Parallel()
	session, out := t.TempDir(), scratch(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Join(out, "sub"), link); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	ps := parts(session, "cd "+filepath.ToSlash(link)+" && rm ../x", false)
	real, _ := filepath.EvalSymlinks(filepath.Join(out, "sub"))
	if got := ps[1].cwds; len(got) != 2 || got[0] != link || got[1] != real {
		t.Errorf("cwds = %q, want [%q %q]", got, link, real)
	}
	ps = parts(session, "cd "+filepath.ToSlash(link)+"/.. && rm x", false)
	if got := ps[1].cwds[0]; got != filepath.Dir(link) {
		t.Errorf("cd link/.. went to %q, want the logical parent %q", got, filepath.Dir(link))
	}
}

// The test name must not hold "cdpath": it is in the temp directory's path.
func TestPartsBareCdSearchPath(t *testing.T) {
	session, out := t.TempDir(), scratch(t)
	o := filepath.ToSlash(out)
	t.Setenv("CDPATH", session)
	ps := parts(session, "cd "+o+" && cd sub && rm x", false)
	if got := ps[2].cwds; len(got) != 1 || got[0] != session {
		t.Errorf("a bare cd under CDPATH: cwds = %q, want the session's", got)
	}
	ps = parts(session, "cd "+o+" && cd ./sub && rm x", false)
	if got := ps[2].cwds[0]; got != filepath.Join(out, "sub") {
		t.Errorf("cd ./sub under CDPATH went to %q", got)
	}
}

func TestPartsCannotEnter(t *testing.T) {
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
	if got := parts(session, "cd "+locked+" && rm x", false)[1].cwds[0]; got != session {
		t.Errorf("a cd into a directory with no search permission went to %q", got)
	}
}

func TestPartsGitBashDrive(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("Git Bash drive paths exist only on Windows")
	}
	session, out := t.TempDir(), scratch(t)
	vol := filepath.VolumeName(out)
	msys := "/" + strings.ToLower(vol[:1]) + filepath.ToSlash(out[len(vol):])
	if got := parts(session, "cd "+msys+" && rm x", false)[1].cwds[0]; !strings.EqualFold(got, out) {
		t.Errorf("cd %s went to %q, want %q", msys, got, out)
	}
	// PowerShell reads /c as a folder at the current drive's root.
	if got := parts(session, "cd "+msys+" && Remove-Item x", true)[1].cwds[0]; got != session {
		t.Errorf("PowerShell cd %s went to %q, want the session's", msys, got)
	}
	if got := parts(session, "cd "+filepath.ToSlash(out[len(vol):])+" && rm x", false)[1].cwds[0]; got != session {
		t.Errorf("a rooted path with no drive went to %q, want the session's", got)
	}
	// /c alone is the drive's root.
	drive := "/" + strings.ToLower(vol[:1])
	if got := parts(session, "cd "+drive+" && rm x", false)[1].cwds[0]; !strings.EqualFold(got, vol+`\`) {
		t.Errorf("cd %s went to %q, want %q", drive, got, vol+`\`)
	}
}

// Windows drive letters are A to Z, in either case (Microsoft Learn,
// "Naming Files, Paths, and Namespaces").
func TestIsLetter(t *testing.T) {
	t.Parallel()
	for _, c := range []byte("azAZ") {
		if !isLetter(c) {
			t.Errorf("isLetter(%q) = false", c)
		}
	}
	for _, c := range []byte("@[`{0/") {
		if isLetter(c) {
			t.Errorf("isLetter(%q) = true", c)
		}
	}
}

// An open double quote that ends in a backslash is still read, not a crash:
// a hook that fails lets the command through.
func TestWordsUnclosedQuoteEndingInBackslash(t *testing.T) {
	t.Parallel()
	if got := words(`echo "a\`, false); len(got) != 2 || got[0] != "echo" {
		t.Errorf("words = %q", got)
	}
}

// GNU Bash manual §3.1.2.3 Double Quotes: inside double quotes a backslash
// followed by a backslash is one backslash. So `cd "<dir>\\sub"` names
// `<dir>\sub`, a path with a backslash in it, which the hook does not follow
// (fail closed, per the rule at the top of cwd.go), and words reads the
// pair once. A words that re-read the pair would never return, so the call
// runs under a deadline, and a miss ends the test binary instead of leaving
// it to spin.
func TestWordsEscapedBackslashInDoubleQuotes(t *testing.T) {
	t.Parallel()
	session, out := t.TempDir(), scratch(t)
	o := filepath.ToSlash(out)
	type result struct {
		ws []string
		ps []part
	}
	done := make(chan result, 1)
	go func() {
		done <- result{words(`echo "a\\b" c`, false), parts(session, `cd "`+o+`\\sub" && rm x`, false)}
	}()
	var r result
	select {
	case r = <-done:
	case <-time.After(2 * time.Second):
		panic(`words did not return on an escaped backslash inside double quotes`)
	}
	if want := []string{"echo", `a\b`, "c"}; strings.Join(r.ws, "|") != strings.Join(want, "|") {
		t.Errorf("words = %q, want %q", r.ws, want)
	}
	if len(r.ps) != 2 || r.ps[1].cwds[0] != session {
		t.Errorf("a cd to a quoted path with a backslash was followed: %+v", r.ps)
	}
}

func TestPartsDirFlags(t *testing.T) {
	t.Parallel()
	session, out := t.TempDir(), scratch(t)
	o := filepath.ToSlash(out)
	ps := parts(session, "cd "+o+" && env -C "+o+"/sub rm x", false)
	want := []string{out, session, filepath.Join(out, "sub")}
	if got := ps[1].cwds; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("env -C: cwds = %q, want %q", got, want)
	}
	ps = parts(session, "make --directory=sub install", false)
	want = []string{session, session, filepath.Join(session, "sub")}
	if got := ps[0].cwds; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("--directory=: cwds = %q, want %q", got, want)
	}
}

func TestPreBashWhereCommandsRun(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	out := scratch(t)
	o := filepath.ToSlash(out)
	root := filepath.ToSlash(r.Root)
	testrepo.Write(t, out, "snapshots/s.txt", "s")
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
		// Commands that surely run outside the repository: allowed.
		{bash, "cd " + o + " && git checkout -q -- .", false},
		{bash, "cd " + o + " && rm -rf snapshots", false},
		{bash, "cd " + o + " && git restore -- . && git status", false},
		{bash, "cd " + o + "/sub && echo x > ../snapshots/s.txt", false},
		{bash, `cd "` + o + `/with space" && rm -rf ../snapshots`, false},
		{bash, "cd " + o + " && go test ./... 2>&1 && git checkout -- .", false},
		{pwsh, `Set-Location -Path "` + out + `" && Remove-Item -Recurse snapshots`, false},
		{pwsh, `cd ` + out + ` && Remove-Item -Recurse snapshots`, false},
		// GNU Bash manual §3.6.8 Duplicating File Descriptors: in [n]<&word
		// the & belongs to the redirect, as in >&; it does not end the chain.
		{bash, "cd " + o + " && cat <&0 && rm -rf snapshots", false},
		// GNU coreutils ls: -C lists entries by columns. A directory flag
		// with no word after it is judged, not read past the line's end.
		{bash, "ls -C", false},

		// A cd inside the repository makes a relative path protected.
		{bash, "cd snapshots && rm s.txt", true},
		{bash, "cd src && rm -rf ../snapshots", true},
		{bash, "cd " + root + "/src/a && cd ../.. && rm -rf snapshots", true},
		{bash, "cd " + o + " && cd " + root + " && rm -rf snapshots", true},
		{bash, "cd " + o + " && X=1 cd " + root + " && rm -rf snapshots", true},
		{bash, "cd " + o + ` && c\d ` + root + " && rm -rf snapshots", true},
		{bash, "cd " + o + ` && "cd" ` + root + " && rm -rf snapshots", true},
		{pwsh, `Set-Location src && Remove-Item -Recurse ..\snapshots`, true},
		{bash, "cd " + o + " && env -C " + root + " rm -rf snapshots", true},

		// GNU Bash manual §3.6 Redirections: a command performs each of its
		// redirections, so a later one writes as surely as the first.
		{bash, "echo hi > out.txt 2> snapshots/s.txt", true},
		{bash, "go test ./... 2>snapshots/s.txt", true},
		{bash, "go test ./... &> snapshots/s.txt", true},
		{bash, "echo hi >| snapshots/s.txt", true},
		{bash, "go test ./... > out.txt 2>&1", false},

		// Fail closed: judged from the session's directory.
		{bash, "cd " + o + " || rm -rf snapshots", true},
		// trackable: a control character (DEL is one) makes the line doubtful.
		{bash, "cd " + o + " && rm -rf snapshots && echo \x7f", true},
		// A line may end in a background & (GNU Bash manual §3.2.4 Lists) or
		// a lone backslash; the hook still judges it rather than failing.
		{bash, "rm -rf snapshots &", true},
		{bash, `rm -rf snapshots \`, true},
		{bash, "cd " + o + "; rm -rf snapshots", true},
		{bash, "cd " + o + "\nrm -rf snapshots", true},
		{bash, "cd " + o + " && true || rm -rf snapshots", true},
		{bash, "true || cd " + o + "; rm -rf snapshots", true},
		{bash, "false && cd " + o + "; rm -rf snapshots", true},
		{bash, "true || cd " + o + " && rm -rf snapshots", true},
		{bash, "cd " + o + " | true && rm -rf snapshots", true},
		{bash, "cd " + o + " & rm -rf snapshots", true},
		{bash, "cd $SCRATCH && rm -rf snapshots", true},
		{bash, "cd - && rm -rf snapshots", true},
		{bash, "cd " + o + " && popd && rm -rf snapshots", true},
		{bash, "cd " + o + "/missing && rm -rf snapshots", true},
		{bash, "cd " + o + "/with space && rm -rf snapshots", true},
		{bash, "cd -P " + o + " && rm -rf snapshots", true},
		{bash, "cd " + o + " && (cd " + root + " && rm -rf snapshots)", true},
		{bash, "cd " + o + " && { cd " + root + "; rm -rf snapshots; }", true},
		{bash, "cd " + o + " && command cd " + root + " && rm -rf snapshots", true},
		{bash, "cd " + o + " && eval cd " + root + " && rm -rf snapshots", true},
		{bash, "alias cd=true\ncd " + o + " && rm -rf snapshots", true},
		{bash, "for i in 1; do cd " + o + " && rm -rf snapshots; done", true},
		{bash, "if true; then cd " + o + "; rm -rf snapshots; fi", true},
		{bash, "! cd " + o + " && rm -rf snapshots", true},
		{bash, `echo x\;cd ` + o + " && rm -rf snapshots", true},
		{bash, `echo "a;cd ` + o + `" && rm -rf snapshots`, true},
		{bash, "cd " + o + " && echo hi\rrm -rf snapshots", true},
		{pwsh, `Set-Location ` + out + `; Remove-Item -Recurse snapshots`, true},
		{pwsh, `Set-Location -StackName ` + out + ` && Remove-Item -Recurse snapshots`, true},
		{pwsh, `Set-Location ` + out + ` && .\setup.ps1 && Remove-Item -Recurse snapshots`, true},
		{pwsh, "echo \u2018; Set-Location " + out + " && \u2019 && Remove-Item -Recurse snapshots", true},
		{pwsh, "echo --% ; Set-Location " + out + " && x | Remove-Item -Recurse snapshots", true},
	}
	for _, tt := range tests {
		d := tt.run(tt.cmd)
		if (d.Deny != "") != tt.deny {
			t.Errorf("%q: deny=%q, want deny=%v", tt.cmd, d.Deny, tt.deny)
		}
	}
}

// A symlink outside the repository that leads into it: Bash resolves .. in
// rm's path through the link, so the write lands in the repository.
func TestPreBashCdThroughSymlink(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Join(r.Root, "src"), link); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	for _, cmd := range []string{
		"cd " + filepath.ToSlash(link) + " && rm -rf ../snapshots",
		"cd " + filepath.ToSlash(link) + " && echo x > ../snapshots/s.txt",
	} {
		if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny == "" {
			t.Errorf("%q was allowed", cmd)
		}
	}
	// A directory holding an oracle, which only the directory check sees.
	r = open(t, "protected_paths: [\"**/*.verified.*\"]\ncommands:\n  build: \"true\"\n")
	testrepo.Write(t, r.Root, "test/Snapshots/A.verified.txt", "x")
	testrepo.Git(t, r.Root, "add", "-A")
	link = filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Join(r.Root, "src"), link); err != nil {
		t.Fatal(err)
	}
	cmd := "cd " + filepath.ToSlash(link) + " && git checkout main -- ../test/Snapshots"
	if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny == "" {
		t.Errorf("%q was allowed", cmd)
	}
}

// Windows and macOS file systems ignore case, so a cd spelled in another
// case still lands in the repository.
func TestPreBashCdOtherCase(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("needs a case-insensitive file system")
	}
	r := open(t, passing)
	cmd := "cd " + strings.ToUpper(filepath.ToSlash(filepath.Join(r.Root, "src"))) + " && rm -rf ../snapshots"
	if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny == "" {
		t.Errorf("%q was allowed", cmd)
	}
}

// A relative path resolves in the session's directory, which the hook
// input's cwd names (knowledge/decisions/hook-decisions-v0.md), not the
// repository root.
func TestPreBashFromASubdirectory(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	src := filepath.Join(r.Root, "src")
	if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: src, ToolInput: hookio.ToolInput{Command: "rm -rf ../snapshots"}}); d.Deny == "" {
		t.Error("rm -rf ../snapshots from src: allowed, want denied")
	}
	if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: src, ToolInput: hookio.ToolInput{Command: "rm -rf snapshots"}}); d.Deny != "" {
		t.Errorf("rm -rf snapshots from src: denied (%s), want allowed: src/snapshots is not protected", d.Deny)
	}
}
