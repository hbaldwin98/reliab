package runner

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hbaldwin98/reliab/internal/gitx"
)

// Target is what a run looks at, already resolved.
type Target struct {
	// Specs are the --target values, or "changed since <base>" / "everything".
	Specs []string
	// All means the whole repository; Files is empty then.
	All bool
	// Files are repo-relative, slash-separated source files.
	Files []string
	// Ranges limits a file to inclusive line ranges; a file absent here
	// counts whole.
	Ranges map[string][][2]int
	// Projects own Files: a project file (.csproj), a Go package (./dir),
	// or a directory holding package.json, Cargo.toml or pyproject.toml.
	Projects []string
	// Tests are the test files, test projects or test packages for Files.
	Tests []string
}

// Contains reports whether a file and line fall inside the target. Line 0
// asks about the file only.
func (t *Target) Contains(file string, line int) bool {
	if t.All {
		return true
	}
	if !contains(t.Files, file) && !contains(t.Tests, file) {
		return false
	}
	rs, ok := t.Ranges[file]
	if !ok || line == 0 {
		return true
	}
	for _, r := range rs {
		if line >= r[0] && line <= r[1] {
			return true
		}
	}
	return false
}

// projectFiles are the names that make a directory a project. Go packages
// are found by their .go files instead.
var projectFiles = []string{"package.json", "Cargo.toml", "pyproject.toml"}

var projectExts = []string{".csproj", ".fsproj", ".vbproj"}

func isProjectFile(p string) bool {
	base := path.Base(p)
	return contains(projectFiles, base) || base == "go.mod" || contains(projectExts, path.Ext(p))
}

// ResolveAll is the target of an audit with no --target.
func ResolveAll() *Target {
	return &Target{Specs: []string{"everything"}, All: true}
}

// ResolveChanged is the target of change and pr with no --target: what the
// branch changed since base, committed or not, limited to changed lines.
func ResolveChanged(root, base string) (*Target, error) {
	lines, err := gitx.ChangedLines(root, base)
	if err != nil {
		return nil, err
	}
	files, err := gitx.ChangedSince(root, base)
	if err != nil {
		return nil, err
	}
	t := &Target{Specs: []string{"changed since " + base}, Ranges: map[string][][2]int{}}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			continue // deleted: nothing to run on
		}
		t.Files = append(t.Files, f)
		if ls, ok := lines[f]; ok && len(ls) > 0 {
			t.Ranges[f] = toRanges(ls)
		}
	}
	return t, t.finish(root)
}

// Resolve turns --target values into a Target. Each spec is a project
// file, a folder, a file, or file#Function.
func Resolve(root string, specs []string) (*Target, error) {
	t := &Target{Specs: specs, Ranges: map[string][][2]int{}}
	seen := map[string]bool{}
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			t.Files = append(t.Files, f)
		}
	}
	for _, spec := range specs {
		p, fn, _ := strings.Cut(spec, "#")
		rel, err := relPath(root, p)
		if err != nil {
			return nil, fmt.Errorf("--target %s: %w", spec, err)
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("--target %s: %w", spec, err)
		}
		switch {
		case fn != "":
			if info.IsDir() {
				return nil, fmt.Errorf("--target %s: a function target names a file, not a folder", spec)
			}
			src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				return nil, err
			}
			start, end, err := FindFunc(rel, src, fn)
			if err != nil {
				return nil, fmt.Errorf("--target %s: %w", spec, err)
			}
			add(rel)
			t.Ranges[rel] = append(t.Ranges[rel], [2]int{start, end})
		case info.IsDir() || isProjectFile(rel):
			dir := rel
			if !info.IsDir() {
				dir = path.Dir(rel)
			}
			files, err := gitx.Files(root, []string{dir})
			if err != nil {
				return nil, err
			}
			if len(files) == 0 {
				return nil, fmt.Errorf("--target %s: no files", spec)
			}
			for _, f := range files {
				add(f)
			}
		default:
			add(rel)
		}
	}
	sort.Strings(t.Files)
	return t, t.finish(root)
}

func relPath(root, p string) (string, error) {
	if !filepath.IsAbs(p) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		p = filepath.Join(wd, p)
	}
	rel, ok := gitx.Rel(root, p)
	if !ok {
		return "", fmt.Errorf("not inside the repository")
	}
	rel = filepath.ToSlash(rel)
	if rel == "" {
		rel = "."
	}
	return rel, nil
}

// finish works out Projects and Tests from Files.
func (t *Target) finish(root string) error {
	projects := map[string]bool{}
	tests := map[string]bool{}
	for _, f := range t.Files {
		if isTestFile(f) && !strings.HasSuffix(f, ".go") {
			tests[f] = true
		}
		if p := owningProject(root, f); p != "" {
			projects[p] = true
		}
		for _, s := range siblingTests(root, f) {
			tests[s] = true
		}
	}
	for p := range projects {
		for _, tp := range testProjects(root, p) {
			tests[tp] = true
		}
	}
	t.Projects = sortedKeys(projects)
	t.Tests = sortedKeys(tests)
	return nil
}

// owningProject walks up from f to the nearest project. A Go file's project
// is its package, which is what go test takes.
func owningProject(root, f string) string {
	dir := path.Dir(f)
	if strings.HasSuffix(f, ".go") {
		if dir == "." {
			return "."
		}
		return "./" + dir
	}
	for {
		entries, _ := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if contains(projectExts, path.Ext(e.Name())) {
				return path.Join(dir, e.Name())
			}
			if contains(projectFiles, e.Name()) {
				return dir
			}
		}
		if dir == "." {
			return ""
		}
		dir = path.Dir(dir)
	}
}

var testNameRe = regexp.MustCompile(`(_test\.go$|Tests?\.(cs|fs|vb)$|\.(test|spec)\.[cm]?[jt]sx?$|(^|/)test_[^/]*\.py$|_test\.py$)`)

func isTestFile(f string) bool { return testNameRe.MatchString(f) }

// siblingTests finds test files named after f next to it: foo.ts has
// foo.test.ts, foo.py has test_foo.py. Go has none here: its package is
// the unit tests run in, and testProjects adds it.
func siblingTests(root, f string) []string {
	if isTestFile(f) {
		return nil
	}
	dir, base := path.Dir(f), path.Base(f)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	var names []string
	switch ext {
	case ".py":
		names = []string{"test_" + base, stem + "_test.py"}
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		names = []string{stem + ".test" + ext, stem + ".spec" + ext}
	case ".cs", ".fs", ".vb":
		names = []string{stem + "Tests" + ext, stem + "Test" + ext}
	}
	var out []string
	for _, n := range names {
		p := path.Join(dir, n)
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// testProjects returns the test projects for a project. A Go package is
// its own test package when it has _test.go files. A .NET project's test
// projects are those that reference it.
func testProjects(root, project string) []string {
	if project == "." || strings.HasPrefix(project, "./") {
		matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(project), "*_test.go"))
		if len(matches) > 0 {
			return []string{project}
		}
		return nil
	}
	if !contains(projectExts, path.Ext(project)) {
		return nil
	}
	if isTestProject(project) {
		return []string{project}
	}
	all, err := gitx.Files(root, []string{"*" + path.Ext(project)})
	if err != nil {
		return nil
	}
	name := path.Base(project)
	var out []string
	for _, p := range all {
		if p == project || !isTestProject(p) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err == nil && referencesProject(string(data), name) {
			out = append(out, p)
		}
	}
	return out
}

func isTestProject(p string) bool {
	return strings.Contains(strings.ToLower(path.Base(p)), "test")
}

var projectRefRe = regexp.MustCompile(`<ProjectReference\s+Include="([^"]+)"`)

func referencesProject(xml, name string) bool {
	for _, m := range projectRefRe.FindAllStringSubmatch(xml, -1) {
		if path.Base(strings.ReplaceAll(m[1], `\`, "/")) == name {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// toRanges collapses a set of line numbers into sorted inclusive ranges.
func toRanges(lines map[int]bool) [][2]int {
	ns := make([]int, 0, len(lines))
	for n := range lines {
		ns = append(ns, n)
	}
	sort.Ints(ns)
	var out [][2]int
	for _, n := range ns {
		if len(out) > 0 && out[len(out)-1][1] == n-1 {
			out[len(out)-1][1] = n
		} else {
			out = append(out, [2]int{n, n})
		}
	}
	return out
}
