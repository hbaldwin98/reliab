package snapdiff

import (
	"os"
	"path/filepath"
	"testing"
)

func file(t *testing.T, dir, name string, body []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCompareFiles(t *testing.T) {
	dir := t.TempDir()
	acc := file(t, dir, "a.verified.txt", []byte("\xEF\xBB\xBF{\r\n  A: 1\r\n}"))
	same := file(t, dir, "same.received.txt", []byte("{\n  A: 1\n}\n"))
	added := file(t, dir, "add.received.txt", []byte("{\n  A: 1,\n  B: 2\n}"))
	changed := file(t, dir, "chg.received.txt", []byte("{\n  A: 2\n}"))
	bin := file(t, dir, "b.received.png", []byte{0x89, 'P', 0, 1})

	for _, c := range []struct {
		name, accepted, pending, label string
		isNew, binary                  bool
	}{
		// A byte-order mark and line endings are not changes.
		{"equal", acc, same, "", false, false},
		{"additive", acc, added, AdditiveOnly, false, false},
		{"changed", acc, changed, ValuesChanged, false, false},
		{"no accepted file", filepath.Join(dir, "none.verified.txt"), added, AdditiveOnly, true, false},
		{"binary", acc, bin, ValuesChanged, false, true},
	} {
		r, err := CompareFiles(c.accepted, c.pending)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if r.Label != c.label || r.New != c.isNew || r.Binary != c.binary {
			t.Errorf("%s: %+v, want label %q new %v binary %v", c.name, r, c.label, c.isNew, c.binary)
		}
	}
	if _, err := CompareFiles(acc, filepath.Join(dir, "missing.received.txt")); err == nil {
		t.Error("want error for a missing pending file")
	}
	// An accepted file that exists but cannot be read is an error, not a
	// new snapshot.
	if _, err := CompareFiles(dir, same); err == nil {
		t.Error("want error when the accepted path cannot be read")
	}
	// A NUL in the first byte makes a file binary too.
	nul := file(t, dir, "nul.received.bin", []byte{0, 'a'})
	if r, err := CompareFiles(acc, nul); err != nil || !r.Binary {
		t.Errorf("leading NUL: %+v, %v", r, err)
	}
}
