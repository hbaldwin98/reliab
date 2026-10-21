package adapters

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// knowledge/decisions/external-adapters.md: reliab's own adapters are
// ordinary external adapters with no special access; they share the public
// pkg/ packages any Go adapter can import. So no adapter's non-test code
// imports an internal package.
func TestInRepoAdaptersUseOnlyPublicPackages(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "cmd", "adapters", "*", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no adapter sources: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); strings.Contains(p, "/internal/") {
				t.Errorf("%s imports %s", filepath.ToSlash(f), p)
			}
		}
	}
}
