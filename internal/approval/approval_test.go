package approval

import (
	"os"
	"path/filepath"
	"testing"
)

// knowledge/decisions/protected-path-approval.md: one approval per path,
// the newest wins, and it matches only the content it approved.
func TestSaveLoadPutMatch(t *testing.T) {
	root := t.TempDir()
	if as, err := Load(root); err != nil || as != nil {
		t.Fatalf("no file: %v, %v", as, err)
	}
	as := Put(nil, Approval{Path: "s/a", BaseBlob: "b0", Blob: "b1", By: "human:a"})
	as = Put(as, Approval{Path: "s/b", Blob: "c1", By: "human:a"})
	as = Put(as, Approval{Path: "s/a", BaseBlob: "b0", Blob: "b2", By: "human:b"})
	if err := Save(root, as); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil || len(got) != 2 {
		t.Fatalf("loaded %+v, %v; want two, one per path", got, err)
	}
	if m := Match(got, "s/a", "b0", "b2"); m == nil || m.By != "human:b" {
		t.Errorf("newest approval of s/a: %+v", m)
	}
	if m := Match(got, "s/b", "", "c1"); m == nil || m.Path != "s/b" {
		t.Errorf("the first approval in the file: %+v", m)
	}
	for _, miss := range [][3]string{{"s/a", "b0", "b1"}, {"s/a", "bX", "b2"}, {"s/c", "", "c1"}, {"s/b", "", ""}} {
		if m := Match(got, miss[0], miss[1], miss[2]); m != nil {
			t.Errorf("Match%v = %+v, want none", miss, m)
		}
	}
}

func TestLoadRefusesAnotherMajor(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, Path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"schemaVersion":"1.0.0","approvals":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Error("read an approvals file of another major version")
	}
}
