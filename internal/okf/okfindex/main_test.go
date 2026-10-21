package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rootIndex = "---\nokf_version: \"0.2\"\n---\n\n# K\n\n* [o](o.md) - x\n\n# Sections\n\n* [Settled choices](d/) - y\n"

func bundle(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// knowledge/decisions/okf-knowledge-bundle.md: `go run
// ./internal/okf/okfindex` writes each section index of the repo's bundle,
// knowledge/, from its concepts' frontmatter; a named bundle is written
// instead.
func TestRunWritesSectionIndexes(t *testing.T) {
	dir := t.TempDir()
	bundle(t, dir, map[string]string{
		"knowledge/index.md":   rootIndex,
		"knowledge/d/index.md": "# stale\n",
		"knowledge/d/a.md":     "---\ntype: Decision\ntitle: Ay\ndescription: first\n---\n",
		"other/index.md":       rootIndex,
		"other/d/index.md":     "# stale\n",
		"other/d/b.md":         "---\ntype: Decision\ntitle: Bee\ndescription: second\n---\n",
	})
	t.Chdir(dir)
	if err := run(nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "knowledge", "d", "index.md")); string(got) != "# Settled choices\n\n* [Ay](a.md) - first\n" {
		t.Errorf("default bundle: knowledge/d/index.md = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "other", "d", "index.md")); string(got) != "# stale\n" {
		t.Errorf("default run touched another bundle: %q", got)
	}
	if err := run([]string{"other"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "other", "d", "index.md")); string(got) != "# Settled choices\n\n* [Bee](b.md) - second\n" {
		t.Errorf("named bundle: other/d/index.md = %q", got)
	}
}

// knowledge/decisions/okf-knowledge-bundle.md: an index comes from every
// concept's frontmatter, and okf.Index refuses a concept with no title
// (internal/okf TestIndex); go generate must fail then, not leave a stale
// index while reporting success. The same holds for a bundle with no root
// index and an index that cannot be written.
func TestRunFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	bundle(t, dir, map[string]string{
		"untitled/index.md":          rootIndex,
		"untitled/d/a.md":            "---\ntype: Decision\n---\n",
		"unwritable/index.md":        rootIndex,
		"unwritable/d/a.md":          "---\ntype: Decision\ntitle: Ay\n---\n",
		"unwritable/d/index.md/keep": "a directory where the index goes",
	})
	for _, name := range []string{"untitled", "unwritable", "missing"} {
		if err := run([]string{filepath.Join(dir, name)}, &bytes.Buffer{}); err == nil {
			t.Errorf("%s: run succeeded", name)
		}
	}
	if got, _ := os.ReadDir(filepath.Join(dir, "untitled", "d")); len(got) != 1 || strings.HasPrefix(got[0].Name(), "index") {
		t.Errorf("untitled: wrote an index anyway: %v", got)
	}
}
