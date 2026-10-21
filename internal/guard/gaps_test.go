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

// git(1) SYNOPSIS: global options such as -C <path>, -c <name>=<value>,
// --git-dir, --work-tree, and --no-pager come before the subcommand, so
// git -C <dir> checkout is the checkout writeVerb names
// (knowledge/decisions/hook-decisions-v0.md: writes to protected paths).
func TestPreBashGitGlobalOptions(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	elsewhere := t.TempDir()
	root := filepath.ToSlash(r.Root)
	tests := []struct {
		cwd, cmd string
		deny     bool
	}{
		{elsewhere, "git -C " + root + " checkout -- snapshots/s.txt", true},
		{elsewhere, "git -C " + root + " restore snapshots", true},
		{elsewhere, "git -C " + root + " clean -fdx snapshots", true},
		{r.Root, "git -c core.autocrlf=false checkout -- snapshots/s.txt", true},
		{r.Root, "git --no-pager rm snapshots/s.txt", true},
		{r.Root, "git --git-dir=.git --work-tree=. checkout -- snapshots/s.txt", true},
		{r.Root, "git --git-dir .git restore snapshots/s.txt", true},
		{r.Root, "git -C src reset --hard && git -C . checkout main -- snapshots", true},
		{elsewhere, "git -C " + root + " log -- snapshots/s.txt", false},
		{elsewhere, "git -C " + root + " checkout -- src/a/a.txt", false},
		{r.Root, "git -c core.pager=cat diff -- snapshots", false},
	}
	for _, tt := range tests {
		d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: tt.cwd, ToolInput: hookio.ToolInput{Command: tt.cmd}})
		if (d.Deny != "") != tt.deny {
			t.Errorf("%q: deny=%q, want deny=%v", tt.cmd, d.Deny, tt.deny)
		}
	}
	// A directory holding an oracle, which only the directory check sees.
	r = open(t, "protected_paths: [\"**/*.verified.*\"]\ncommands:\n  build: \"true\"\n")
	testrepo.Write(t, r.Root, "test/Snapshots/A.verified.txt", "x")
	testrepo.Git(t, r.Root, "add", "-A")
	cmd := "git -C " + filepath.ToSlash(r.Root) + " checkout main -- test/Snapshots"
	if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: elsewhere, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny == "" {
		t.Errorf("%q was allowed", cmd)
	}
}

// Windows' file systems ignore case, so a protected path spelled in another
// case is the same file (TestPreBashCdOtherCase; knowledge/decisions/
// hook-decisions-v0.md: edits and writes to protected paths are denied).
func TestProtectedPathOtherCase(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("needs a case-insensitive file system")
	}
	r := open(t, passing)
	for _, p := range []string{"SNAPSHOTS/s.txt", "Snapshots/new.txt", ".Reliab/reports/inner-latest.json"} {
		if d := r.PreEdit(hookio.Input{Cwd: r.Root, ToolInput: hookio.ToolInput{FilePath: p}}); d.Deny == "" {
			t.Errorf("edit %s was allowed", p)
		}
	}
	for _, cmd := range []string{"rm SNAPSHOTS/s.txt", "echo x > Snapshots/s.txt", "rm -rf .RELIAB"} {
		if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny == "" {
			t.Errorf("%q was allowed", cmd)
		}
	}
	r = open(t, "protected_paths: [\"**/*.verified.*\"]\ncommands:\n  build: \"true\"\n")
	testrepo.Write(t, r.Root, "test/Snapshots/A.verified.txt", "x")
	testrepo.Git(t, r.Root, "add", "-A")
	for _, cmd := range []string{"rm test/snapshots/a.VERIFIED.txt", "rm -rf TEST/SNAPSHOTS", "git checkout main -- test/snapshots"} {
		if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny == "" {
			t.Errorf("%q was allowed", cmd)
		}
	}
	if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: "rm -rf src"}}); d.Deny != "" {
		t.Errorf("rm -rf src: denied (%s)", d.Deny)
	}
}

// path_resolution(7): the kernel resolves each component through symlinks
// before the next, so a .. after a symlink leaves the directory the link
// leads to, not the one it is named in. A write there lands where the
// kernel resolves it (TestPreBashCdThroughSymlink does this for a cd).
func TestProtectedPathThroughSymlink(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	testrepo.Write(t, r.Root, "snapshots/sub/t.txt", "t")
	in := filepath.Join(r.Root, "src", "l")
	if err := os.Symlink(filepath.Join(r.Root, "snapshots", "sub"), in); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	out := filepath.Join(t.TempDir(), "l")
	if err := os.Symlink(filepath.Join(r.Root, "snapshots", "sub"), out); err != nil {
		t.Fatal(err)
	}
	outs := filepath.ToSlash(out)
	for _, cmd := range []string{
		"rm src/l/../s.txt",
		"echo x > src/l/../s.txt",
		"rm src/l/t.txt",
		"rm " + outs + "/../s.txt",
		"cp x " + outs + "/../new.txt",
	} {
		if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny == "" {
			t.Errorf("%q was allowed", cmd)
		}
	}
	for _, p := range []string{"src/l/../s.txt", outs + "/../new.txt"} {
		if d := r.PreEdit(hookio.Input{Cwd: r.Root, ToolInput: hookio.ToolInput{FilePath: p}}); d.Deny == "" {
			t.Errorf("edit %s was allowed", p)
		}
	}
	for _, cmd := range []string{"rm src/a/../b/b.txt", "rm src/l/../../../" + strings.Repeat("x", 3)} {
		if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny != "" {
			t.Errorf("%q: denied (%s)", cmd, d.Deny)
		}
	}
}

// knowledge/decisions/hook-decisions-v0.md: a path is judged as the kernel
// resolves it through symlinks. A file a write creates does not exist yet,
// but the directories naming it do, so a new file named through a link
// lands where the link leads.
func TestProtectedNewFileThroughSymlink(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	testrepo.Write(t, r.Root, "snapshots/sub/t.txt", "t")
	if err := os.Symlink(filepath.Join(r.Root, "snapshots", "sub"), filepath.Join(r.Root, "src", "l")); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	for _, cmd := range []string{"echo x > src/l/new.txt", "cp x src/l/new/deeper.txt"} {
		if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny == "" {
			t.Errorf("%q was allowed", cmd)
		}
	}
	if d := r.PreEdit(hookio.Input{Cwd: r.Root, ToolInput: hookio.ToolInput{FilePath: "src/l/new.txt"}}); d.Deny == "" {
		t.Error("edit src/l/new.txt was allowed")
	}
}

// NTFS on Windows and APFS as macOS formats it by default ignore case
// (Microsoft, "Case sensitivity"; Apple, "File system formats available in
// Disk Utility"); Linux file systems do not.
func TestCaselessOS(t *testing.T) {
	t.Parallel()
	for goos, want := range map[string]bool{"windows": true, "darwin": true, "linux": false, "freebsd": false} {
		if got := caselessOS(goos); got != want {
			t.Errorf("caselessOS(%q) = %v, want %v", goos, got, want)
		}
	}
}
