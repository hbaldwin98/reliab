package okf

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// The section indexes are generated from the concepts' frontmatter, and a
// test fails when a checked-in one is out of date
// (knowledge/decisions/okf-knowledge-bundle.md).
func TestRepoIndexesCurrent(t *testing.T) {
	fsys := os.DirFS("../../knowledge")
	dirs, headings, err := Sections(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 7 {
		t.Fatalf("root index lists %d sections: %v", len(dirs), dirs)
	}
	for i, dir := range dirs {
		want, err := Index(fsys, dir, headings[i])
		if err != nil {
			t.Fatal(err)
		}
		have, err := os.ReadFile("../../knowledge/" + dir + "/index.md")
		if err != nil {
			t.Fatal(err)
		}
		if strings.ReplaceAll(string(have), "\r\n", "\n") != string(want) {
			t.Errorf("knowledge/%s/index.md is out of date; run go generate ./internal/okf", dir)
		}
	}
}

func TestIndex(t *testing.T) {
	concept := func(title, desc string) *fstest.MapFile {
		return file("---\ntype: Decision\ntitle: \"" + title + "\"\ndescription: \"" + desc + "\"\n---\n\n# " + title + "\n")
	}
	fsys := fstest.MapFS{
		"index.md":      file("---\nokf_version: \"0.2\"\n---\n\n# K\n\n* [o](o.md) - x\n\n# Sections\n\n* [Settled choices](d/) - y\n* [Other](e/) - z\n"),
		"d/index.md":    file("# stale\n"),
		"d/b.md":        concept("Bee", "second"),
		"d/a.md":        file(strings.ReplaceAll("---\ntype: Decision\ntitle: Ay\ndescription: first\n---\n", "\n", "\r\n")),
		"d/log.md":      file("# Log\n"),
		"d/sub/c.md":    concept("Nested", "not listed"),
		"d/notes.txt":   file("not a concept"),
		"e/no-desc.md":  file("---\ntype: Concept\ntitle: Bare\n---\n"),
		"f/untitled.md": file("---\ntype: Concept\n---\n"),
	}
	dirs, headings, err := Sections(fsys)
	if err != nil || strings.Join(dirs, ",") != "d,e" || strings.Join(headings, ",") != "Settled choices,Other" {
		t.Fatalf("Sections = %v %v %v", dirs, headings, err)
	}
	got, err := Index(fsys, "d", "Settled choices")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Settled choices\n\n* [Ay](a.md) - first\n* [Bee](b.md) - second\n"; string(got) != want {
		t.Errorf("Index(d) = %q, want %q", got, want)
	}
	if got, err := Index(fsys, "e", "Other"); err != nil || string(got) != "# Other\n\n* [Bare](no-desc.md)\n" {
		t.Errorf("Index(e) = %q, %v", got, err)
	}
	if _, err := Index(fsys, "f", "F"); err == nil || !strings.Contains(err.Error(), "no title") {
		t.Errorf("untitled concept: err = %v", err)
	}
	// A generated index conforms to the structure Check enforces.
	errs, _ := Check(fstest.MapFS{"index.md": file(string(got))})
	for _, e := range errs {
		t.Error(e)
	}
}
