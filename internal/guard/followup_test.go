package guard

import (
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
)

// An independent review of the guard-effects branch (2026-10-20) found
// these lines judged differently from what knowledge/decisions/
// command-effects.md promises: writes the one reader should see, pushes the
// pre-push gate should get (knowledge/decisions/pre-push-v0.md: a push or
// PR creation), and false positives the user ranks as costly as a bypass.
func TestPreBashReviewWrites(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	tests := []struct {
		tool, cmd string
	}{
		// A function's body runs when it is called (GNU Bash manual §3.3),
		// and agents define helpers such as retry() in a line.
		{"Bash", "retry() { rm -rf snapshots; }; retry"},
		{"Bash", "f() { cp x snapshots/s.txt; }; f"},
		{"Bash", "f () { cp x snapshots/s.txt; }; f"},
		{"Bash", "function f { cp x snapshots/s.txt; }; f"},
		{"Bash", "function f() { cp x snapshots/s.txt; }; f"},
		// Tee-Object writes the file it names (PowerShell
		// about_Redirection; Tee-Object -FilePath).
		{"PowerShell", "Tee-Object -FilePath snapshots/s.txt"},
		{"PowerShell", "'x' | Tee-Object snapshots/s.txt"},
		// Code nested in watch, sh -c --, coproc, and a sourced process
		// substitution runs (command-effects §1).
		{"Bash", "watch -n1 'cp x snapshots/s.txt'"},
		{"Bash", "watch -n 1 -d 'cp x snapshots/s.txt'"},
		{"Bash", "sh -c -- 'cp x snapshots/s.txt'"},
		{"Bash", "coproc cp x snapshots/s.txt"},
		{"Bash", ". <(echo cp x snapshots/s.txt)"},
		{"Bash", "source <(printf 'cp x snapshots/s.txt')"},
		{"Bash", "bash <(echo cp x snapshots/s.txt)"},
		// Globs and braces expand before rm or cp runs (§3.5.1, §3.5.8).
		{"Bash", "rm -rf ./*"},
		{"Bash", "rm -rf snap*"},
		{"Bash", "cp x snap{shots,}/s.txt"},
		{"PowerShell", "Remove-Item -Recurse snap*"},
	}
	for _, tt := range tests {
		d := r.PreBash(hookio.Input{ToolName: tt.tool, Cwd: r.Root, ToolInput: hookio.ToolInput{Command: tt.cmd}})
		if !strings.Contains(d.Deny, "snapshots") {
			t.Errorf("%s %q: deny=%q, want a denial naming snapshots", tt.tool, tt.cmd, d.Deny)
		}
	}
}

// knowledge/decisions/pre-push-v0.md gates every push: git subtree push
// pushes to a remote (git-subtree(1)), and a git alias without ! runs the
// git subcommand it names (git-config(1) alias.*).
func TestPreBashReviewPushes(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	for _, cmd := range []string{
		"git subtree push --prefix x origin main",
		"git -c alias.p=push p",
		"git -c alias.p='push -f' p origin main",
	} {
		d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}})
		if d.Deny == "" {
			t.Errorf("%q: allowed, want the pre-push gate", cmd)
		}
	}
	// ...but a subtree or alias that does not push is no push.
	for _, cmd := range []string{"git subtree split --prefix x", "git -c alias.s=status s"} {
		if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny != "" {
			t.Errorf("%q: denied (%s)", cmd, d.Deny)
		}
	}
}
