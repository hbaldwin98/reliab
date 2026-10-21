package repocheck

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// runHook feeds a hook script the stdin Claude Code gives it and returns
// its stdout. A .awk script runs as awk -f, as the project settings run it.
func runHook(t *testing.T, script string, input any, env ...string) string {
	t.Helper()
	interp, args := "sh", []string{}
	if strings.HasSuffix(script, ".awk") {
		interp, args = "awk", []string{"-f"}
	}
	bin, err := exec.LookPath(interp)
	if err != nil {
		t.Skip("no " + interp)
	}
	// Claude Code writes <, >, and & as they are; Go's default escapes them.
	var raw strings.Builder
	enc := json.NewEncoder(&raw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(input); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".claude", "hooks", script)
	// Reading the script makes go test's cache key on it; the shell's read
	// would not, and a cached pass would hide an edit.
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, append(args, path)...)
	cmd.Stdin = strings.NewReader(raw.String())
	cmd.Env = append(os.Environ(), env...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s exited %v (it must always exit 0): %s", script, err, stderr.String())
	}
	return string(out)
}

// The dev repo's shell-write hook warns, and never blocks, when a command
// writes a .go or .md file around the editor tools; reading such a file, a
// commit message, or a heredoc body that only mentions one stays quiet
// (knowledge/decisions/dev-environment.md).
func TestShellWriteHookWarns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tool, command string
		warn          bool
	}{
		{"Bash", "cat > internal/x.go <<'EOF'\npackage x\nEOF", true},
		{"Bash", "cat <<EOF >> knowledge/log.md\n* entry\nEOF", true},
		{"Bash", `python3 -c "open('README.md','w').write('x')"`, true},
		{"Bash", "python - <<'EOF'\nimport pathlib\npathlib.Path(\"docs/adapters.md\").write_text(s)\nEOF", true},
		{"Bash", "sed -i 's/a/b/' internal/guard/guard.go", true},
		{"Bash", "sed -i.bak -e 's/a/b/' AGENTS.md", true},
		{"Bash", "perl -pi -e 's/a/b/' docs/windows.md", true},
		{"Bash", "echo hi >> knowledge/log.md", true},
		{"Bash", `printf '%s' x > "internal/okf/okf.go"`, true},
		{"Bash", "printf x | tee -a AGENTS.md", true},
		{"PowerShell", "Set-Content -Path README.md -Value $text", true},
		{"PowerShell", "$s | Out-File -Encoding utf8 docs\\windows.md", true},
		{"PowerShell", "[IO.File]::WriteAllText('C:\\src\\a.go', $s)", true},

		{"Bash", "go test ./... 2>&1 | grep _test.go", false},
		{"Bash", "go test ./internal/okf/", false},
		{"Bash", "cat README.md", false},
		{"Bash", "sed -n 1,10p internal/okf/okf.go", false},
		{"Bash", `python3 -c "print(open('README.md').read())"`, false},
		{"Bash", "go run ./cmd/reliab report route > out.txt; cat main.go", false},
		{"Bash", "grep -n x a.go > out.txt", false},
		{"Bash", `echo "a -> b.md"`, false},
		{"Bash", "awk 1 a.go >a.go.new", false},
		{"Bash", "git add a.go && git commit -m \"Docs: never cat > notes.md\"", false},
		{"Bash", "cat <<'EOF' > notes.txt\nwrite to a.md with > a.md\nEOF", false},
		{"Bash", "gh pr create --body \"sed -i fixed README.md\"", false},
		{"PowerShell", "Get-Content README.md | Select-Object -First 5", false},

		// Review findings: an in-place flag is an option word, not any
		// hyphenated path; python warns only on code that writes a .go or
		// .md file; a commit's text is data however git is spelled.
		{"Bash", "sed -i 's/a/b/' knowledge/decisions/dev-environment.md", true},
		{"Bash", "git -C . commit -m x && sed -i s/a/b/ a.go", true},
		{"Bash", "echo x > a.md # done", true},
		{"Bash", "cat <<EOF>>knowledge/log.md\n* entry\nEOF", true},
		{"Bash", "cat <<-EOF > a.go\n\tpackage a\n\tEOF", true},
		{"Bash", `echo x > "notes dir/a.md"`, true},
		{"Bash", "git commit -F - <<'EOF'\nverify: cat > a.md\nEOF", false},
		{"PowerShell", `"see a.md" | Out-File notes.txt`, false},
		{"Bash", "sed -n 1,40p knowledge/decisions/dev-environment.md", false},
		{"Bash", "sed -n 1,40p knowledge/decisions/inner-tier-trigger.md", false},
		{"Bash", "sed -n 1,40p .claude/worktrees/dev-integration/x.go", false},
		{"Bash", "sed -n 1,40p C:/src/reliab/.claude/worktrees/small-fixes/internal/guard/guard.go", false},
		{"Bash", "perl -MList::Util -ne 'print' docs/windows.md", false},
		{"Bash", `python -c "import sys; sys.stdout.write(open('README.md').read())"`, false},
		{"Bash", "python3 - <<'EOF'\nopen('out.txt','w').write(open('README.md').read())\nEOF", false},
		{"Bash", `git log --oneline -5 && grep -rn "write" --include=*.go internal/okf | head; python3 --version`, false},
		{"Bash", "cat > notes.txt <<EOF\nDo not use python to write README.md\nEOF", false},
		{"Bash", `git -C . commit -m "cat > a.md"`, false},
		{"Bash", "echo hi # > a.md", false},
		{"PowerShell", "Get-Content a.md | Set-Content -Path b.txt", false},
		{"PowerShell", "git -C . commit -m \"[IO.File]::WriteAllText('a.go', $s)\"", false},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			t.Parallel()
			in := map[string]any{
				"session_id": "s", "cwd": root, "hook_event_name": "PreToolUse",
				"tool_name": tt.tool, "tool_input": map[string]any{"command": tt.command, "description": "d"},
				"tool_use_id": "t",
			}
			out := runHook(t, "warn-shell-writes.awk", in)
			if !tt.warn {
				if out != "" {
					t.Errorf("%s %q: warned %q, want silence", tt.tool, tt.command, out)
				}
				return
			}
			var res struct {
				Decision string `json:"decision"`
				HSO      struct {
					HookEventName      string `json:"hookEventName"`
					AdditionalContext  string `json:"additionalContext"`
					PermissionDecision string `json:"permissionDecision"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal([]byte(out), &res); err != nil {
				t.Fatalf("%s %q: output %q is not JSON: %v", tt.tool, tt.command, out, err)
			}
			if res.HSO.HookEventName != "PreToolUse" || !strings.Contains(res.HSO.AdditionalContext, "not blocked") ||
				res.HSO.PermissionDecision != "" || res.Decision != "" {
				t.Errorf("%s %q: output %+v, want a PreToolUse warning without a decision", tt.tool, tt.command, res)
			}
		})
	}
}

// The hook runs on every shell call, large heredocs among them, so its cost
// must grow with the command's length, not its square: a 200 KB heredoc
// stays far inside the hook's 10 s timeout (knowledge/decisions/
// dev-environment.md; review finding on quadratic cost).
func TestShellWriteHookLinear(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("a line that names README.md and a.go but writes notes.txt\n", 3500)
	in := map[string]any{
		"hook_event_name": "PreToolUse", "tool_name": "Bash",
		"tool_input": map[string]any{"command": "cat > notes.txt <<'EOF'\n" + body + "EOF"},
	}
	start := time.Now()
	if out := runHook(t, "warn-shell-writes.awk", in); out != "" {
		t.Errorf("warned %q, want silence", out)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("a %d KB command took %v", len(body)/1024, d)
	}
}

// The session-start hook builds this checkout's reliab into .devbin and
// puts it on the session's PATH through CLAUDE_ENV_FILE; without go it
// still exits 0 (knowledge/decisions/dev-environment.md).
func TestDevSessionStartHook(t *testing.T) {
	if testing.Short() {
		t.Skip("builds reliab")
	}
	envFile := filepath.Join(t.TempDir(), "env")
	in := map[string]any{"session_id": "s", "hook_event_name": "SessionStart", "source": "startup"}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	bin := filepath.Join(abs, ".devbin", "reliab"+ext)
	if out := runHook(t, "dev-session-start.sh", in, "CLAUDE_PROJECT_DIR="+abs, "CLAUDE_ENV_FILE="+envFile); out != "" {
		t.Fatalf("build reported %q", out)
	}
	env, err := os.ReadFile(envFile)
	if err != nil || !strings.Contains(string(env), "export PATH=") || !strings.Contains(string(env), "/.devbin:") {
		t.Fatalf("env file = %q, %v", env, err)
	}
	if out, err := exec.Command(bin, "version").CombinedOutput(); err != nil || !strings.Contains(string(out), "v0.0.0-dev") {
		t.Errorf("dev build: %v %s", err, out)
	}

	// No go on PATH: nothing to build, and the session still starts with
	// .devbin on PATH.
	noGo := filepath.Join(t.TempDir(), "env")
	if out := runHook(t, "dev-session-start.sh", in, "CLAUDE_PROJECT_DIR="+abs, "CLAUDE_ENV_FILE="+noGo, "PATH="); out != "" {
		t.Errorf("without go: output %q", out)
	}
	if env, err := os.ReadFile(noGo); err != nil || !strings.Contains(string(env), "/.devbin:") {
		t.Errorf("without go: env file = %q, %v", env, err)
	}

	// A failed build tells the agent, as JSON it can read, and still exits 0.
	if out := runHook(t, "dev-session-start.sh", in, "CLAUDE_PROJECT_DIR="+abs, "CLAUDE_ENV_FILE="+envFile, "GOFLAGS=-mod=bogus"); !json.Valid([]byte(out)) || !strings.Contains(out, "go run ./cmd/reliab") {
		t.Errorf("failed build: output %q", out)
	}
}
