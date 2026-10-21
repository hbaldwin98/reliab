package guard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
