package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

// A repository reached through a symlinked directory (macOS's /var is
// /private/var) is still the same repository: a file named through the alias
// is inside it, including one not created yet, and a sibling is not.
func TestRelSeesThroughAnAlias(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(filepath.Join(real, "repo", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	root := filepath.Join(real, "repo")
	for _, c := range []struct {
		p    string
		want string
		ok   bool
	}{
		{filepath.Join(alias, "repo", "src"), "src", true},
		{filepath.Join(alias, "repo", "src", "new", "file.go"), "src/new/file.go", true},
		{filepath.Join(root, "src"), "src", true},
		{filepath.Join(alias, "elsewhere"), "", false},
		{filepath.Join(real, "repo-sibling", "x"), "", false},
	} {
		got, ok := Rel(root, c.p)
		if got != c.want || ok != c.ok {
			t.Errorf("Rel(%s) = %q %v, want %q %v", c.p, got, ok, c.want, c.ok)
		}
	}
	// And with the root itself named through the alias.
	if got, ok := Rel(filepath.Join(alias, "repo"), filepath.Join(root, "src")); got != "src" || !ok {
		t.Errorf("aliased root: %q %v", got, ok)
	}
}
