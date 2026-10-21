package okf

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

//go:generate go run ./okfindex ../../knowledge

// sectionRe reads a section entry of the bundle-root index: its link text
// and its directory.
var sectionRe = regexp.MustCompile(`^[*-] \[([^\]]+)\]\(([^)/\s]+)/\)`)

// Sections returns the directories the bundle-root index lists as sections,
// each with its link text, in the root index's order.
func Sections(fsys fs.FS) (dirs, headings []string, err error) {
	raw, err := fs.ReadFile(fsys, "index.md")
	if err != nil {
		return nil, nil, err
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			dirs = append(dirs, m[2])
			headings = append(headings, m[1])
		}
	}
	return dirs, headings, nil
}

// Index renders dir's index.md from its concepts' frontmatter: the heading
// is the root index's link text for the section, and each concept is a
// "* [title](file.md) - description" entry, in file-name order.
func Index(fsys fs.FS, dir, heading string) ([]byte, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && path.Ext(n) == ".md" && n != "index.md" && n != "log.md" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("# " + heading + "\n\n")
	for _, n := range names {
		raw, err := fs.ReadFile(fsys, path.Join(dir, n))
		if err != nil {
			return nil, err
		}
		front, _, ok := splitFrontmatter(strings.ReplaceAll(string(raw), "\r\n", "\n"))
		if !ok {
			return nil, fmt.Errorf("%s/%s: missing YAML frontmatter", dir, n)
		}
		var fm struct{ Title, Description string }
		if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
			return nil, fmt.Errorf("%s/%s: %w", dir, n, err)
		}
		if strings.TrimSpace(fm.Title) == "" {
			return nil, fmt.Errorf("%s/%s: frontmatter has no title", dir, n)
		}
		b.WriteString("* [" + fm.Title + "](" + n + ")")
		if d := strings.TrimSpace(fm.Description); d != "" {
			b.WriteString(" - " + d)
		}
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}
