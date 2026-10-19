package findings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Adapters write per-file counts so reliab can score a baseline scope
// (knowledge/decisions/mutation-threshold.md); without any, the field is
// left out rather than written empty.
func TestWriteMutantFiles(t *testing.T) {
	dir := t.TempDir()
	read := func(name string) map[string]any {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	counts := &MutantCounts{Killed: 2, Survived: 1}
	if err := WriteMutantFiles(filepath.Join(dir, "a.json"), nil, counts, map[string]MutantCounts{"src/a.go": {Killed: 2, Survived: 1}}); err != nil {
		t.Fatal(err)
	}
	files, ok := read("a.json")["mutantFiles"].(map[string]any)
	if !ok || files["src/a.go"] == nil {
		t.Errorf("mutantFiles = %v", read("a.json")["mutantFiles"])
	}
	if err := WriteMutantFiles(filepath.Join(dir, "b.json"), nil, counts, map[string]MutantCounts{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := read("b.json")["mutantFiles"]; ok {
		t.Error("empty per-file counts were written")
	}
	if doc := read("b.json"); doc["schemaVersion"] != SchemaVersion || doc["mutants"] == nil {
		t.Errorf("doc = %v", doc)
	}
}
