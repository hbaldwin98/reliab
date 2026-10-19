// Package gitx wraps the few git operations reliab needs: the repository
// root, the working-tree hash that makes reports fresh or stale, and the set
// of changed files.
package gitx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Root returns the top-level directory of the repository containing dir.
func Root(dir string) (string, error) {
	out, err := git(dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(strings.TrimSpace(out)), nil
}

// TreeHash returns the git tree hash of the working tree: tracked and
// untracked non-ignored files, minus paths matching exclude globs. It builds
// the tree in a temporary index, seeded from the real one for speed, so the
// user's staging area is never touched. See
// knowledge/decisions/report-freshness.md.
func TreeHash(root string, exclude []string) (string, error) {
	tmp, err := os.CreateTemp("", "reliab-index-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if real, err := git(root, nil, "rev-parse", "--path-format=absolute", "--git-path", "index"); err == nil {
		real = strings.TrimSpace(real)
		if b, err := os.ReadFile(real); err == nil {
			if err := os.WriteFile(tmpPath, b, 0o600); err != nil {
				return "", err
			}
			// Git re-reads an entry whose mtime is not older than the index
			// file's own ("racily clean"). The copy keeps the real index's
			// mtime, or a same-size edit in that second would go unseen.
			if fi, err := os.Stat(real); err == nil {
				if err := os.Chtimes(tmpPath, fi.ModTime(), fi.ModTime()); err != nil {
					return "", err
				}
			}
		} else {
			_ = os.Remove(tmpPath) // no index yet: git creates a fresh one
		}
	}
	env := []string{"GIT_INDEX_FILE=" + tmpPath}
	if _, err := git(root, env, "add", "--all", "--", "."); err != nil {
		return "", err
	}
	if len(exclude) > 0 {
		args := []string{"rm", "--cached", "-r", "-q", "--ignore-unmatch", "--"}
		for _, g := range exclude {
			args = append(args, ":(glob)"+g)
		}
		if _, err := git(root, env, args...); err != nil {
			return "", err
		}
	}
	out, err := git(root, env, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Head returns the commit HEAD names, or "" in a repository with no commits.
func Head(root string) string {
	out, err := git(root, nil, "rev-parse", "-q", "--verify", "HEAD^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// IsAncestor reports whether commit a is an ancestor of, or equal to, b.
func IsAncestor(root, a, b string) bool {
	_, err := git(root, nil, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// MergeBase returns the best common ancestor of ref and HEAD.
func MergeBase(root, ref string) (string, error) {
	out, err := git(root, nil, "merge-base", ref, "HEAD")
	return strings.TrimSpace(out), err
}

// BaseFallbacks are the refs ResolveBase tries, in order, when neither a
// flag nor the profile names a base.
var BaseFallbacks = []string{"origin/HEAD", "origin/main", "main", "master"}

// ResolveBase returns the ref a branch is measured from: explicit (a
// command-line flag) if set, else profile (the profile's base), else the
// first of BaseFallbacks that has a merge base with HEAD. A named ref must
// have one too.
func ResolveBase(root, explicit, profile string) (string, error) {
	for _, ref := range []string{explicit, profile} {
		if ref == "" {
			continue
		}
		if _, err := MergeBase(root, ref); err != nil {
			return "", fmt.Errorf("no merge base with %s: %w", ref, err)
		}
		return ref, nil
	}
	for _, ref := range BaseFallbacks {
		if _, err := MergeBase(root, ref); err == nil {
			return ref, nil
		}
	}
	return "", fmt.Errorf("no base ref found (tried %s); set base in the profile or pass --base", strings.Join(BaseFallbacks, ", "))
}

// hunk is a unified diff hunk header: the new side's start and length.
var hunk = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// ChangedLines returns the lines the working tree adds or changes relative
// to the merge base of ref and HEAD, by repo-relative slash path. Every
// line of an untracked non-ignored file counts as changed; a file moved
// without edits has no changed lines.
func ChangedLines(root, ref string) (map[string]map[int]bool, error) {
	base, err := MergeBase(root, ref)
	if err != nil {
		return nil, err
	}
	// -M: a moved file's unchanged lines are not changes.
	out, err := git(root, nil, "diff", "-M", "--no-color", "--no-ext-diff", "-U0", base)
	if err != nil {
		return nil, err
	}
	lines := map[string]map[int]bool{}
	file := ""
	// if, not switch: Go coverage does not instrument case expressions, so
	// gremlins would call their mutants not covered.
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSuffix(l, "\r")
		if strings.HasPrefix(l, "+++ ") {
			// A deleted file's new side is /dev/null: it has no lines.
			file = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
			if file == "/dev/null" {
				file = ""
			}
			continue
		}
		m := hunk.FindStringSubmatch(l)
		if m == nil || file == "" {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		n := 1
		if m[2] != "" {
			n, _ = strconv.Atoi(m[2])
		}
		if lines[file] == nil {
			lines[file] = map[int]bool{}
		}
		for i := start; i < start+n; i++ {
			lines[file][i] = true
		}
	}
	untracked, err := git(root, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(untracked, "\x00") {
		if p == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			continue
		}
		n := strings.Count(string(raw), "\n")
		if len(raw) > 0 && raw[len(raw)-1] != '\n' {
			n++ // a last line without a newline
		}
		all := map[int]bool{}
		for i := 1; i <= n; i++ {
			all[i] = true
		}
		lines[p] = all
	}
	return lines, nil
}

// CommonDir returns the absolute path of the repository's shared git
// directory, which every worktree of one repository has in common.
func CommonDir(dir string) (string, error) {
	out, err := git(dir, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Clean(strings.TrimSpace(out)), nil
}

// ChangedFiles returns repo-relative, slash-separated paths that differ
// between HEAD and the working tree, including untracked non-ignored files.
// In a repository with no commits every non-ignored file counts as changed.
func ChangedFiles(root string) ([]string, error) {
	out, err := git(root, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		status, path := e[:2], e[3:]
		seen[path] = true
		if status[0] == 'R' || status[0] == 'C' {
			i++ // the next entry is the rename or copy source
			if i < len(entries) && entries[i] != "" {
				seen[entries[i]] = true
			}
		}
	}
	files := make([]string, 0, len(seen))
	for f := range seen {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

func git(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ChangedSince returns repo-relative paths that differ between the merge
// base of ref and HEAD, and the working tree: everything a branch changed
// relative to ref, committed or not.
func ChangedSince(root, ref string) ([]string, error) {
	base, err := git(root, nil, "merge-base", ref, "HEAD")
	if err != nil {
		return nil, err
	}
	out, err := git(root, nil, "diff", "--name-only", "--no-renames", "-z", strings.TrimSpace(base))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			seen[p] = true
		}
	}
	untracked, err := git(root, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(untracked, "\x00") {
		if p != "" {
			seen[p] = true
		}
	}
	files := make([]string, 0, len(seen))
	for f := range seen {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

// Files returns the repo-relative, slash-separated paths of tracked and
// untracked non-ignored files under paths.
func Files(root string, paths []string) ([]string, error) {
	out, err := git(root, nil, append([]string{"ls-files", "--cached", "--others", "--exclude-standard", "-z", "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			seen[p] = true
		}
	}
	files := make([]string, 0, len(seen))
	for f := range seen {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

// WorkingBlob returns the git blob hash of the file at the repo-relative
// path as it is in the working tree, or "" when there is no such file.
func WorkingBlob(root, path string) (string, error) {
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	out, err := git(root, nil, "hash-object", "--", path)
	return strings.TrimSpace(out), err
}

// FileAt returns the content of the repo-relative path in commit.
func FileAt(root, commit, path string) ([]byte, error) {
	out, err := git(root, nil, "show", commit+":"+path)
	return []byte(out), err
}

// BlobAt returns the blob hash of the repo-relative path in commit, or ""
// when the commit has no such file.
func BlobAt(root, commit, path string) string {
	out, err := git(root, nil, "rev-parse", "-q", "--verify", commit+":"+path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
