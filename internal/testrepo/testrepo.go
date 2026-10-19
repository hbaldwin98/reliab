// Package testrepo builds throwaway git repositories with a reliab profile
// for tests.
package testrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Roles fills every tool role, so test profiles only add what they need.
const Roles = `roles:
  T1: { tool: sh }
  T2: { applicable: false, reason: no property library }
  T3: { applicable: false, reason: none }
  T4: { applicable: false, reason: none }
  T5: { applicable: false, reason: none }
  T6: { applicable: false, reason: none }
  T7: { applicable: false, reason: none }
  T8: { applicable: false, reason: none }
  T9: { applicable: false, reason: none }
  T10: { applicable: false, reason: none }
  T11: { applicable: false, reason: none }
  T12: { applicable: false, reason: none }
  T13: { applicable: false, reason: none }
  T14: { tool: sh }
`

// New creates a committed repository whose profile is a fixed header plus
// extra YAML (commands, tiers, impacted, protected_paths), and returns its
// root. Product code lives in src/, tests in test/, docs are ignored.
func New(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	prof := "schemaVersion: 0.1.0\nname: test\nstarting_condition: greenfield\n" +
		"paths: { product: [\"src/**\"], tests: [\"test/**\"], ignore: [\"docs/**\"] }\n" + Roles + extra
	Write(t, dir, "reliability-profile.yaml", prof)
	Write(t, dir, ".gitignore", ".reliab/\n")
	Write(t, dir, "src/a/a.txt", "a")
	Write(t, dir, "src/b/b.txt", "b")
	Write(t, dir, "snapshots/s.txt", "s")
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
