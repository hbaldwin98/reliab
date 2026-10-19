package guard

import (
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
)

// knowledge/decisions/hook-decisions-v0.md: the person-only commands are
// denied where a shell may run them, and only there; their words inside data
// (a commit message, a heredoc fed to git, an echo or grep argument) pass.
// When the scanner cannot be sure the text is data, it denies. A session was
// denied a commit whose heredoc message named the approve command; a review
// of the first fix found the bypasses listed under "Reviewer's bypasses".
func TestPreBashPersonOnlyInData(t *testing.T) {
	r := open(t, passing)
	tests := []struct {
		tool string // the hook's tool_name; empty means Bash
		cmd  string
		deny bool
	}{
		// Data: allowed.
		{"", `git commit -m "Docs: a person runs reliab approve PATH"`, false},
		{"", "git add a b && git commit -q -F - <<'EOF'\nSay so. A person runs reliab approve.\nEOF\ngit log --oneline -1; git status --short", false},
		{"", "git commit -F - <<EOF\nreliab findings ack is people-only\nEOF", false},
		{"", "git commit -F - <<-EOF\n\treliab approve x\n\tEOF", false},
		{"", "git commit -F - << 'EOF'\nreliab approve x --by human:me\nEOF", false},
		{"", "git tag -a v1 -m 'reliab baseline write is people-only'", false},
		{"", `git commit -m "fix #12 (docs): reliab approve {path}"`, false},
		{"", `echo "reliab findings ack F-x --by human:me"`, false},
		{"", "echo reliab approve x", false},
		{"", `echo "say \"reliab approve x\""`, false},
		{"", `echo reliab\ approve x`, false},
		{"", `echo 'literal $(reliab approve x --by human:me)'`, false},
		{"", `echo "reliab approve x" || echo no`, false},
		{"", `echo "reliab approve x";`, false},
		{"", `grep -rn "reliab approve" plugin`, false},
		{"", `grep "reliab report verdict" a.md | grep -v x`, false},
		{"", `echo ok && git commit -m "reliab approve"`, false},
		// A quoted or escaped delimiter leaves the body unexpanded.
		{"", "cat <<\\EOF\n$(reliab approve x --by human:me)\nEOF", false},
		{"", "git commit -F - <<\"EOF\"\n$(reliab approve x --by human:me)\nEOF", false},
		// Bash ends a heredoc with no closing line at the end of input.
		{"", "git commit -F - <<EOF\nreliab approve x --by human:me", false},
		// Bash reads these PowerShell tricks as plain text.
		{"", `echo "\"; reliab approve x; echo "\""`, false},
		{"", "echo hi\rreliab approve x", false},
		{"", "echo '\u2018; reliab approve x; \u2018'", false},

		// Code a shell runs: denied.
		{"", `bash -c "reliab approve x --by human:me"`, true},
		{"", `sh -c 'reliab findings ack F-x --by human:me'`, true},
		{"", `pwsh -Command "reliab approve x --by human:me"`, true},
		{"", `powershell -c "reliab baseline write"`, true},
		{"", `cmd /c "reliab approve x --by human:me"`, true},
		{"", `eval "reliab approve x --by human:me"`, true},
		{"", `echo "$(reliab approve x --by human:me)"`, true},
		{"", "echo `reliab approve x --by human:me`", true},
		{"", `git commit -m "$(reliab approve x --by human:me)"`, true},
		{"", `cat <(reliab approve x --by human:me)`, true},
		{"", "sh <<EOF\nreliab approve x --by human:me\nEOF", true},
		{"", "bash -s <<'EOF'\nreliab findings ack F-x --by human:me\nEOF", true},
		{"", "sh <<EOF\nreliab approve x --by human:me", true},
		{"", "cat <<'EOF' | sh\nreliab approve x --by human:me\nEOF", true},
		{"", "cat <<EOF\n$(reliab approve x --by human:me)\nEOF", true},
		{"", "git commit -F - <<EOF\n`reliab approve x --by human:me`\nEOF", true},
		{"", `echo "reliab approve x --by human:me" | sh`, true},
		{"", `echo "reliab approve x --by human:me" |& sh`, true},
		{"", `sh <<< "reliab approve x --by human:me"`, true},
		{"", `git commit -m "x" && reliab approve x --by human:me`, true},
		{"", `echo ok; reliab approve x --by human:me`, true},
		{"", `if true; then reliab approve x --by human:me; fi`, true},
		{"", `./echo "reliab approve x --by human:me"`, true},
		{"", `git -c alias.x='!reliab approve y --by human:me' x`, true},
		{"", `git rebase --exec "reliab approve x --by human:me" main`, true},
		{"", `xargs -I{} reliab approve {} --by human:me < list`, true},
		// <<'' is a heredoc ended by an empty line; the rest of the line runs.
		{"", "cat <<'' ; reliab approve x --by human:me", true},
		// A body ends at its closing line, so what follows is judged.
		{"", "git commit -F - <<'EOF'\nmsg\nEOF\nreliab approve x --by human:me", true},
		{"", "cat <<\\EOF\nmsg\nEOF\nreliab approve x --by human:me", true},
		{"", "git commit -F - <<EOF\n\nmsg\nEOF\nreliab approve x --by human:me", true},
		{"", "git commit -F - <<EOF\r\nmsg\r\nEOF\r\nreliab approve x --by human:me", true},
		// Something shadows a data command, or stores the text to run later.
		{"", "echo() { sh -c \"$1\"; }; echo 'reliab approve x --by human:me'", true},
		{"", "PATH=/tmp/x:$PATH cat <<EOF\nreliab approve x --by human:me\nEOF", true},
		{"", "PATH=/tmp/x; echo 'reliab approve x --by human:me'", true},
		{"", "printf -v x 'reliab approve y --by human:me'; $x", true},
		{"", "X=reliab approve=1", true},
		// A malformed line still gets a decision, never a panic
		// (hook-decisions-v0: never exit 2 on a hook error).
		{"", "reliab approve x --by human:me <<", true},
		{"", "reliab approve x --by human:me <<\\", true},
		{"", "reliab approve x --by human:me; git", true},
		// The existing forms.
		{"", "/usr/local/bin/reliab approve x --by human:me", true},
		{"", `& "C:\tools\reliab.exe" approve x --by human:me`, true},
		{"", "RELIAB_BIN=x reliab baseline write", true},
		{"", "cd x && RELIAB=1 reliab.cmd  APPROVE tests/seed --by human:me", true},
		{"", "reliab report verdict --outcome pass --by me --notes ok", true},
		// In doubt: a redirect could write a script, so data beside one is
		// judged.
		{"", `git commit -m "reliab approve" 2>&1`, true},

		// Reviewer's bypasses of the first fix, each verified to run the
		// hidden command in Bash or pwsh.
		{"", `echo $'\'' ; reliab approve x #'`, true},
		{"", "echo # '\nreliab approve x # '", true},
		{"", "echo hi # <<EOF\nreliab approve x\nEOF", true},
		{"", `echo 'reliab approve x' > r.sh; sh r.sh`, true},
		{"", "cat <<'EOF' > r.sh\nreliab approve x\nEOF\nbash r.sh", true},
		{"", `printf 'reliab approve x' >> ~/.bashrc`, true},
		{"PowerShell", `echo (reliab approve x)`, true},
		{"PowerShell", `Write-Output @(reliab approve x)`, true},
		{"PowerShell", `echo @{a=reliab approve x}`, true},
		{"PowerShell", "echo '\u2018; reliab approve x; \u2018'", true},
		{"PowerShell", "echo hi\rreliab approve x", true},
		{"PowerShell", `echo "\"; reliab approve x; echo "\""`, true},
		// codeText models Bash only: PowerShell, or a tool it does not know,
		// is judged whole.
		{"PowerShell", `git commit -m "reliab approve"`, true},
		{"PowerShell", "git commit -m @'\nreliab approve x\n'@", true},
		{"Other", `echo "reliab approve x"`, true},
	}
	for _, tt := range tests {
		tool := tt.tool
		if tool == "" {
			tool = "Bash"
		}
		d := r.PreBash(hookio.Input{Cwd: r.Root, ToolName: tool, ToolInput: hookio.ToolInput{Command: tt.cmd}})
		if (d.Deny != "") != tt.deny {
			t.Errorf("%s %q: deny=%q, want deny=%v", tool, tt.cmd, d.Deny, tt.deny)
		}
	}
}
