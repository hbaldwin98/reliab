// Package testrepo builds throwaway git repositories for tests.
package testrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// New creates a committed repository and returns its root. config, when
// not empty, is written as reliab.yaml. src/a and src/b hold a file each.
func New(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	if config != "" {
		Write(t, dir, "reliab.yaml", config)
	}
	Write(t, dir, ".gitignore", ".reliab/\n")
	Write(t, dir, "src/a/a.txt", "a")
	Write(t, dir, "src/b/b.txt", "b")
	Git(t, dir, "init", "-q", "-b", "main")
	Git(t, dir, "add", "-A")
	Git(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "init")
	return dir
}

// Write creates or replaces a file under dir.
func Write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Git runs a git command in dir, with a fixed identity so commits work on a
// machine with no git user configured (a CI runner).
func Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}
