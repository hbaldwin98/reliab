package setup

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/hbaldwin98/reliab/internal/profile"
)

// selfGlob is the protected_paths entry that makes the profile guard its
// own threshold, relevance rules, and baseline
// (knowledge/decisions/protected-path-approval.md).
const selfGlob = profile.DefaultPath

var (
	topKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*:`)
	// protected_paths: [a, "b"]   # comment
	flowLine = regexp.MustCompile(`^(protected_paths\s*:\s*\[)([^\]#]*)(\].*)$`)
	// protected_paths:            # comment
	blockKey  = regexp.MustCompile(`^protected_paths\s*:\s*(#.*)?$`)
	blockItem = regexp.MustCompile(`^(\s*)-\s`)
)

// ensureSelfProtected adds the profile to an existing profile's
// protected_paths, editing the text so its formatting and comments stay.
// A profile written before the profile protected itself would otherwise
// stay editable by agents. It returns the profile as now written, and a
// warning instead of an edit when the text has a shape it cannot edit
// safely.
func ensureSelfProtected(root string, p *profile.Profile) (*profile.Profile, string, error) {
	if profile.Protects(p, selfGlob) {
		return p, "", nil
	}
	path := filepath.Join(root, profile.DefaultPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return p, "", err
	}
	edited, ok := addSelfGlob(raw)
	if ok {
		q, err := profile.Parse(edited)
		if err == nil && slices.Equal(q.ProtectedPaths, append(slices.Clone(p.ProtectedPaths), selfGlob)) {
			return q, "added " + selfGlob + " to protected_paths: the profile now guards its own threshold, relevance rules, and baseline", os.WriteFile(path, edited, 0o644)
		}
	}
	return p, fmt.Sprintf("WARNING: %s does not protect itself, and init could not add it safely. Add %q to protected_paths by hand: until then an agent can edit its thresholds, relevance rules, and baseline", profile.DefaultPath, selfGlob), nil
}

// addSelfGlob adds selfGlob to the protected_paths in raw: to a one-line
// flow list, after the last item of a block list, or as a new key when
// there is none. ok is false for any other shape.
func addSelfGlob(raw []byte) ([]byte, bool) {
	nl := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		nl = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	join := func(ls []string) ([]byte, bool) { return []byte(strings.Join(ls, nl)), true }
	for i, line := range lines {
		if m := flowLine.FindStringSubmatch(line); m != nil {
			items := strings.TrimRight(m[2], " \t")
			if strings.TrimSpace(items) == "" {
				items = selfGlob
			} else {
				items += ", " + selfGlob
			}
			lines[i] = m[1] + items + m[3]
			return join(lines)
		}
		if blockKey.MatchString(line) {
			last, indent := -1, ""
			for j := i + 1; j < len(lines); j++ {
				l := lines[j]
				if m := blockItem.FindStringSubmatch(l); m != nil {
					last, indent = j, m[1]
					continue
				}
				if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
					continue
				}
				if topKey.MatchString(l) {
					break
				}
				// A continuation line or a nested value: not a shape to edit.
				return nil, false
			}
			if last < 0 {
				return nil, false
			}
			lines = slices.Insert(lines, last+1, indent+"- "+selfGlob)
			return join(lines)
		}
		if strings.HasPrefix(line, "protected_paths") {
			return nil, false
		}
	}
	// No protected_paths key: add one at the end.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	lines = append(lines, "", "# Oracles that change only with human approval (R2). The profile guards", "# its own thresholds, relevance rules, and baseline.", "protected_paths: ["+selfGlob+"]", "")
	return join(lines)
}
