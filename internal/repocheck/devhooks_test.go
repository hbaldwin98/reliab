package repocheck

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
