package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// knowledge/decisions/command-effects.md: one reader serves the person-only
// check and the protected-path check, so a respelling the shell undoes
// (GNU Bash manual §3.1.2.1 line continuation, §3.1.2 quoting and escapes;
// PowerShell about_Special_Characters: a backtick before a newline
// continues the line; about_Quoting_Rules: curly quotes are quotes) hides a
// write no better than it hid reliab findings ack. An architecture review
// of v0.1.5 got each of these writes past the guard while the person-only
// spellings beside them were denied.
func TestPreBashRespelledWrites(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	tests := []struct {
		tool, cmd string
	}{
		{"Bash", "cp x \\\n snapshots/s.txt"},
		{"Bash", "tee \\\n snapshots/s.txt"},
		{"Bash", "bash -c 'cp x snapshots/s.txt'"},
		{"Bash", `cp x sn\apshots/s.txt`},
		{"PowerShell", "Copy-Item x `\n snapshots/s.txt"},
		{"PowerShell", "Copy-Item x “snapshots/s.txt”"},
		{"Bash", "rm \\\n snapshots/s.txt"},
	}
	for _, tt := range tests {
		d := r.PreBash(hookio.Input{ToolName: tt.tool, Cwd: r.Root, ToolInput: hookio.ToolInput{Command: tt.cmd}})
		// Denied for the right reason: the stray backslash of a
		// continuation once read as the repository's root.
		if !strings.Contains(d.Deny, "snapshots/s.txt") {
			t.Errorf("%s %q: deny=%q, want a denial naming snapshots/s.txt", tt.tool, tt.cmd, d.Deny)
		}
	}
	// The same stray backslash denied a write to any file once the root
	// held an oracle.
	if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: "rm \\\n src/a/a.txt"}}); d.Deny != "" {
		t.Errorf("rm of src/a/a.txt across a continuation: denied (%s)", d.Deny)
	}
}

// Regression cases from the retro 2026-10-20: real sessions where the guard
// denied a legitimate command (a false positive costs as much as a bypass),
// or let a protected write through.
func TestPreBashRetro20261020(t *testing.T) {
	t.Parallel()
	r := open(t, "protected_paths: [\"snapshots/**\", \"reliability-profile.yaml\"]\ncommands:\n  build: \"true\"\n")
	other := scratch(t)
	o := filepath.ToSlash(other)
	sample := filepath.ToSlash(filepath.Join(other, "sample"))
	bash := func(cmd string) hookio.Decision {
		return r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}})
	}
	tests := []struct {
		cmd  string
		deny bool
	}{
		// "reliab approve" in a commit message is data
		// (knowledge/decisions/hook-decisions-v0.md: only text Bash may
		// run). Each of these spellings of a commit was denied.
		{`git commit -m "Docs: a person runs reliab approve PATH"`, false},
		{"cd " + o + ` && git add -A && git commit -m "Guard: a person runs reliab approve"`, false},
		{"git add -A && git commit -m \"$(cat <<'EOF'\nGuard: a person runs reliab approve\n\nCo-Authored-By: x\nEOF\n)\"", false},
		{"git -C " + o + ` commit -m "Guard: deny reliab findings ack to agents"`, false},
		// ...while a substitution that runs reliab is still code.
		{"git commit -m \"$(reliab approve x --by human:me)\"", true},
		{"cd " + o + " && reliab approve x --by human:me", true},
		// "git push" in test data or a message is not a push
		// (knowledge/decisions/pre-push-v0.md gates a push or PR creation).
		{`go test ./internal/hookio -run TestInput -args '{"tool_input":{"command":"git push origin main"}}'`, false},
		{`echo '{"tool_name":"Bash","tool_input":{"command":"git push"}}' | go run ./cmd/reliab hook pre-bash`, false},
		{`printf '%s' '{"command":"git push"}' > in.json`, false},
		{`git commit -m "Guard: deny git push without a fresh prepr"`, false},
		{"git log --oneline --grep push", false},
		// A throwaway sample repository outside this one is not this
		// repository's profile, though the line makes it first.
		{"mkdir -p " + sample + " && cd " + sample + " && git init -q && printf 'name: sample\\n' > reliability-profile.yaml", false},
		{"mkdir -p " + sample + " && cd " + sample + " && echo x >> reliability-profile.yaml", false},
		// ...but a mkdir that may have failed moves nothing.
		{"mkdir -p " + sample + "; cd " + sample + " && echo x > reliability-profile.yaml", true},
		{"mkdir -p sample2 && cd sample2 && echo x > ../reliability-profile.yaml", true},
		// reliab's own outputs are always protected
		// (knowledge/decisions/report-storage.md).
		{"rm -rf .reliab/characterize", true},
		{"rm -rf \\\n .reliab/characterize", true},
		{"find .reliab/characterize -delete", true},
		{"find .reliab -name '*.json' -exec rm {} +", true},
		// A heredoc ends at its first closing line (GNU Bash manual
		// §3.6.6); what follows runs, and is judged.
		{"cat > notes.txt <<'EOF'\nnotes\nEOF\nrm -rf snapshots\nEOF", true},
		{"git commit -F - <<'EOF'\nmsg\nEOF\nreliab approve x --by human:me\nEOF", true},
		// ...and a body is data until then, even when it names a write.
		{"cat > notes.md <<'EOF'\nTo reset, rm snapshots/s.txt\nEOF", false},
	}
	for _, tt := range tests {
		if d := bash(tt.cmd); (d.Deny != "") != tt.deny {
			t.Errorf("%q: deny=%q, want deny=%v", tt.cmd, d.Deny, tt.deny)
		}
	}
	// A cd into .reliab, once it exists, moves the write there.
	testrepo.Write(t, r.Root, ".reliab/characterize/a.txt", "x")
	if d := bash("cd .reliab && rm -rf characterize"); d.Deny == "" {
		t.Error("cd .reliab && rm -rf characterize was allowed")
	}
	// The Edit tool on a sample repository's profile.
	if err := os.MkdirAll(filepath.Join(other, "sample"), 0o755); err != nil {
		t.Fatal(err)
	}
	if d := r.PreEdit(hookio.Input{Cwd: r.Root, ToolInput: hookio.ToolInput{FilePath: filepath.Join(other, "sample", "reliability-profile.yaml")}}); d.Deny != "" {
		t.Errorf("edit of a sample repository's profile: denied (%s)", d.Deny)
	}
	// .reliab/ is protected even where no protected file is tracked, so
	// the directory check, which reads git's file list, never decides it.
	bare := open(t, "protected_paths: [\"**/*.verified.*\"]\ncommands:\n  build: \"true\"\n")
	for _, cmd := range []string{"rm -rf .reliab/characterize", "rm -rf \\\n .reliab/characterize", "rm -rf .reliab"} {
		if d := bare.PreBash(hookio.Input{ToolName: "Bash", Cwd: bare.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny == "" {
			t.Errorf("%q was allowed", cmd)
		}
	}
}
