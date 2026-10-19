// Package okf checks an Open Knowledge Format (v0.2) bundle for conformance:
// every concept has parseable frontmatter with a non-empty type, and index.md
// and log.md files follow their reserved structure. Broken links are
// reported as warnings, since the spec allows them.
package okf

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Problem is one conformance error or warning, located by bundle path.
type Problem struct {
	Path string
	Msg  string
}

func (p Problem) String() string { return p.Path + ": " + p.Msg }

// Check walks the bundle rooted at fsys and returns conformance errors and
// link warnings.
func Check(fsys fs.FS) (errs, warns []Problem) {
	walkErr := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".md" {
			return nil
		}
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		var e []string
		switch path.Base(p) {
		case "index.md":
			e = checkIndex(text, p == "index.md")
		case "log.md":
			e = checkLog(text)
		default:
			e = checkConcept(text)
		}
		for _, msg := range e {
			errs = append(errs, Problem{p, msg})
		}
		for _, msg := range brokenLinks(fsys, p, text) {
			warns = append(warns, Problem{p, msg})
		}
		return nil
	})
	if walkErr != nil {
		errs = append(errs, Problem{".", walkErr.Error()})
	}
	return errs, warns
}

// splitFrontmatter returns the YAML between leading "---" fences and the
// body after them. ok is false when the text has no frontmatter block.
func splitFrontmatter(text string) (front, body string, ok bool) {
	if !strings.HasPrefix(text, "---\n") {
		return "", text, false
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		if strings.HasSuffix(rest, "\n---") {
			return rest[:len(rest)-len("\n---")], "", true
		}
		return "", text, false
	}
	return rest[:end], rest[end+len("\n---\n"):], true
}

func checkConcept(text string) []string {
	front, _, ok := splitFrontmatter(text)
	if !ok {
		return []string{"missing YAML frontmatter"}
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return []string{"frontmatter does not parse: " + err.Error()}
	}
	if t, _ := fm["type"].(string); strings.TrimSpace(t) == "" {
		return []string{"frontmatter has no non-empty type"}
	}
	return nil
}

var (
	headingRe = regexp.MustCompile(`^#{1,6} \S`)
	bulletRe  = regexp.MustCompile(`^[*-] \S`)
	linkRe    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	dateRe    = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2})\s*$`)
)

// checkIndex enforces spec §8: headings followed by bulleted link lists, and
// no frontmatter except okf_version on the bundle-root index.
func checkIndex(text string, isRoot bool) []string {
	var errs []string
	if front, body, ok := splitFrontmatter(text); ok {
		var fm map[string]any
		if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
			errs = append(errs, "frontmatter does not parse: "+err.Error())
		}
		for k := range fm {
			if !isRoot || k != "okf_version" {
				errs = append(errs, fmt.Sprintf("index frontmatter key %q not allowed", k))
			}
		}
		text = body
	}
	sawHeading := false
	for i, line := range strings.Split(text, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
		case headingRe.MatchString(line):
			sawHeading = true
		case bulletRe.MatchString(line) && linkRe.MatchString(line):
			if !sawHeading {
				errs = append(errs, fmt.Sprintf("line %d: entry before any heading", i+1))
			}
		default:
			errs = append(errs, fmt.Sprintf("line %d: not a heading or link entry", i+1))
		}
	}
	return errs
}

// checkLog enforces spec §9: date-grouped bullet entries, newest first. An
// optional level-1 title may open the file.
func checkLog(text string) []string {
	var errs []string
	var prev time.Time
	sawDate := false   // a valid date heading has been seen (for ordering)
	inSection := false // any "## " heading has been seen (for entries)
	for i, line := range strings.Split(text, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
		case strings.HasPrefix(line, "# ") && !inSection:
		case strings.HasPrefix(line, "## "):
			inSection = true
			m := dateRe.FindStringSubmatch(line)
			if m == nil {
				errs = append(errs, fmt.Sprintf("line %d: date heading must be ## YYYY-MM-DD", i+1))
				continue
			}
			d, err := time.Parse("2006-01-02", m[1])
			if err != nil {
				errs = append(errs, fmt.Sprintf("line %d: %v", i+1, err))
				continue
			}
			if sawDate && !d.Before(prev) {
				errs = append(errs, fmt.Sprintf("line %d: dates must be strictly newest first", i+1))
			}
			prev, sawDate = d, true
		case bulletRe.MatchString(line):
			if !inSection {
				errs = append(errs, fmt.Sprintf("line %d: entry before any date heading", i+1))
			}
		default:
			errs = append(errs, fmt.Sprintf("line %d: not a date heading or entry", i+1))
		}
	}
	return errs
}

// brokenLinks returns a message for each local link whose target is missing.
// Bundle-relative links start with "/"; others resolve from the file's
// directory.
func brokenLinks(fsys fs.FS, file, text string) []string {
	var msgs []string
	for _, m := range linkRe.FindAllStringSubmatch(text, -1) {
		link := m[1]
		if strings.Contains(link, "://") || strings.HasPrefix(link, "mailto:") || strings.HasPrefix(link, "#") {
			continue
		}
		target, _, _ := strings.Cut(link, "#")
		if strings.HasPrefix(target, "/") {
			target = path.Clean(strings.TrimPrefix(target, "/"))
		} else {
			target = path.Join(path.Dir(file), target)
		}
		if target == "" {
			target = "."
		}
		if _, err := fs.Stat(fsys, target); err != nil {
			msgs = append(msgs, "broken link "+link)
		}
	}
	return msgs
}
