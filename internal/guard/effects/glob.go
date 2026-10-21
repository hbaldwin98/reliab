package effects

import (
	"path/filepath"
	"strings"
)

// maxBraces bounds the words one brace expansion yields, so a pathological
// word still ends.
const maxBraces = 64

// expand returns paths with what the shell may expand each to before the
// command sees it: Bash's brace alternatives (GNU Bash manual §3.5.1) and
// the files a glob matches in each directory the command may run in
// (§3.5.8; PowerShell's cmdlets expand wildcards in -Path themselves), as
// absolute paths. A path with no { or glob character costs nothing; a glob
// lists one directory per component. A quoted glob is expanded too: judging
// a file the command would not touch only errs toward a denial.
func expand(paths, dirs []string, sh Shell) []string {
	var out []string
	for _, p := range paths {
		alts := []string{p}
		if sh != PowerShell && strings.Contains(p, "{") {
			alts = braces(p)
		}
		for _, a := range alts {
			if a != p {
				out = append(out, a)
			}
			if strings.ContainsAny(a, "*?[") {
				out = append(out, globs(a, dirs)...)
			}
		}
	}
	if len(out) == 0 {
		return paths
	}
	return append(paths, out...)
}

// braces expands a word's comma alternatives, a{b,c}d to abd and acd,
// nested ones too; a sequence ({1..3}) or a lone brace stays as written.
func braces(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		depth, end := 0, -1
		var commas []int
		for j := i; j < len(s) && end < 0; j++ {
			if s[j] == '{' {
				depth++
			} else if s[j] == '}' {
				depth--
				if depth == 0 {
					end = j
				}
			} else if s[j] == ',' && depth == 1 {
				commas = append(commas, j)
			}
		}
		if end < 0 || len(commas) == 0 {
			continue
		}
		var out []string
		start := i + 1
		for _, c := range append(commas, end) {
			for _, x := range braces(s[:i] + s[start:c] + s[end+1:]) {
				if len(out) < maxBraces {
					out = append(out, x)
				}
			}
			start = c + 1
		}
		return out
	}
	return []string{s}
}

// globs returns the files a glob matches from each directory, absolute. A
// * or ? does not match a leading dot, as in Bash without dotglob.
func globs(pat string, dirs []string) []string {
	var out []string
	pat = filepath.FromSlash(strings.ReplaceAll(pat, `\`, "/"))
	for _, dir := range dirs {
		if dir == Anywhere {
			continue
		}
		full := pat
		if !filepath.IsAbs(full) {
			full = filepath.Join(dir, pat)
		}
		full = filepath.Clean(full)
		matches, err := filepath.Glob(full)
		if err != nil {
			continue
		}
		want := strings.Split(full, string(filepath.Separator))
		for _, m := range matches {
			if !hidden(want, strings.Split(m, string(filepath.Separator))) {
				out = append(out, m)
			}
		}
	}
	return out
}

// hidden reports whether a match has a component starting with a dot where
// its pattern's component does not.
func hidden(pat, got []string) bool {
	if len(pat) != len(got) {
		return false
	}
	for i := range pat {
		if strings.HasPrefix(got[i], ".") && !strings.HasPrefix(pat[i], ".") {
			return true
		}
	}
	return false
}
