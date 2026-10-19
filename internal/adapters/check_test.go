package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkRoot is a repository root holding src/a.rs, for path checks.
func checkRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.rs"), []byte("fn a() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func findingsFile(body string) []byte {
	return []byte(`{"schemaVersion":"0.4.0",` + body + `}`)
}

// A file an adapter gets right has no warnings: stable test names, paths
// relative to the root (or absolute inside it, which reliab makes relative),
// and counts that match the findings.
func TestCheckFindingsClean(t *testing.T) {
	root := checkRoot(t)
	abs := filepath.ToSlash(filepath.Join(root, "src", "a.rs"))
	c, err := CheckFindings(findingsFile(`"mutants":{"killed":3,"survived":2},
	  "mutantFiles":{"src/a.rs":{"killed":3,"survived":2}},
	  "findings":[{"test":"src/a.rs: replace + with -","location":"src/a.rs:1:4"},
	    {"test":"src/a.rs: replace + with - (#2)","location":"`+abs+`:1:9"}]`), root)
	if err != nil || len(c.Findings) != 2 || c.Mutants == nil || c.Mutants.Killed != 3 || len(c.Warnings) != 0 {
		t.Fatalf("check = %+v, %v", c, err)
	}
}

// Each hazard gets its own warning, naming what to fix.
func TestCheckFindingsWarnings(t *testing.T) {
	root := checkRoot(t)
	for name, tc := range map[string]struct{ body, want string }{
		"line in test":     {`"findings":[{"test":"src/a.rs:12: replace + with -"}]`, "names a line"},
		"line word":        {`"findings":[{"test":"parse fails at line 7"}]`, "names a line"},
		"root in test":     {`"findings":[{"test":"` + filepath.ToSlash(root) + `/src/a.rs: x"}]`, "path from this machine"},
		"drive in test":    {`"findings":[{"test":"D:\\work\\a.rs: x"}]`, "path from this machine"},
		"package-relative": {`"findings":[{"test":"a.rs: x","location":"a.rs:1:1"}]`, "a.rs is not a file in the repository"},
		"outside root":     {`"findings":[{"test":"x","location":"` + filepath.ToSlash(filepath.Dir(root)) + `/elsewhere.rs:1"}]`, "outside the repository"},
		"too few findings": {`"mutants":{"killed":1,"survived":2,"notCovered":1},"findings":[{"test":"x"}]`, "2 survived and 1 not covered, but the file has 1 findings"},
		"files disagree":   {`"mutants":{"killed":1,"survived":0},"mutantFiles":{"src/a.rs":{"killed":2,"survived":0}},"findings":[]`, "add up to 2 killed"},
		"files no totals":  {`"mutantFiles":{"src/a.rs":{"killed":2,"survived":0}},"findings":[]`, "without mutants"},
		"files missing":    {`"mutants":{"killed":2,"survived":0},"mutantFiles":{"a.rs":{"killed":2,"survived":0}},"findings":[]`, `mutantFiles "a.rs"`},
	} {
		c, err := CheckFindings(findingsFile(tc.body), root)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], tc.want) {
			t.Errorf("%s: warnings = %q, want one containing %q", name, c.Warnings, tc.want)
		}
	}
}

// Without a root, paths are not checked; names still are.
func TestCheckFindingsNoRoot(t *testing.T) {
	c, err := CheckFindings(findingsFile(`"findings":[{"test":"x","location":"nowhere.rs:1"},{"test":"y line 3"}]`), "")
	if err != nil || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "names a line") {
		t.Fatalf("check = %+v, %v", c, err)
	}
}

// A file verify would reject is an error, not a warning.
func TestCheckFindingsRejects(t *testing.T) {
	for _, body := range []string{`nope`, `{"findings":[]}`, `{"schemaVersion":"0.4.0","findings":[{"test":"a"},{"test":"a"}]}`} {
		if _, err := CheckFindings([]byte(body), ""); err == nil {
			t.Errorf("%s: want error", body)
		}
	}
}

// A test holding the repository's absolute path is machine-specific even
// where that path has no drive letter, as on a Linux runner
// (knowledge/decisions/external-adapters.md: findings check warns about a
// test that holds a path from this machine).
func TestCheckFindingsRootWithoutDrive(t *testing.T) {
	c, err := CheckFindings(findingsFile(`"findings":[{"test":"/home/ci/repo/src/a.rs: x"}]`), "/home/ci/repo")
	if err != nil || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "path from this machine") {
		t.Fatalf("check = %+v, %v", c, err)
	}
}
