package snapdiff

import (
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// The labels' meaning is stated in knowledge/decisions/snapshot-diff-labels.md
// and the roadmap's step 2: "additive only" is new lines and nothing else
// changed; any removed or changed line is "values changed or removed".

// textLines draws snapshot-like lines: a small alphabet so that repeated
// lines (which make the line matching ambiguous) are common.
func textLines(t *rapid.T, label string) []string {
	return rapid.SliceOfN(rapid.StringMatching(`[ab{}" ]{0,4}`), 0, 12).Draw(t, label)
}

func join(ls []string) string {
	if len(ls) == 0 {
		return ""
	}
	return strings.Join(ls, "\n") + "\n"
}

// Source: the decision: an unchanged snapshot is not a diff.
func TestPropertyEqualIsNoDiff(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := join(textLines(t, "lines"))
		if r := Compare(s, s); r.Label != "" || r.Added != 0 || r.Removed != 0 {
			t.Fatalf("Compare(s, s) = %+v", r)
		}
	})
}

// Source: the decision: lines added and nothing removed or changed is
// additive only, wherever the new lines go.
func TestPropertyInsertionsAreAdditive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		old := textLines(t, "old")
		cur := slices.Clone(old)
		n := rapid.IntRange(1, 4).Draw(t, "inserts")
		for range n {
			at := rapid.IntRange(0, len(cur)).Draw(t, "at")
			cur = slices.Insert(cur, at, rapid.StringMatching(`[abc{}" ,]{0,5}`).Draw(t, "line"))
		}
		r := Compare(join(old), join(cur))
		if r.Label != AdditiveOnly || r.Removed != 0 || r.Added != n {
			t.Fatalf("got %+v, want additive-only with %d added\nold %q\ncur %q", r, n, old, cur)
		}
	})
}

// Source: the decision: any removed line means values changed or removed.
func TestPropertyRemovalsAreValuesChanged(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		old := textLines(t, "old")
		if len(old) == 0 {
			old = []string{"a"}
		}
		cur := slices.Clone(old)
		at := rapid.IntRange(0, len(cur)-1).Draw(t, "at")
		cur = slices.Delete(cur, at, at+1)
		// Anything may be added elsewhere too; the removal still decides.
		if rapid.Bool().Draw(t, "add") {
			cur = append(cur, "new")
		}
		if r := Compare(join(old), join(cur)); r.Label != ValuesChanged || r.Removed == 0 {
			t.Fatalf("got %+v, want values-changed\nold %q\ncur %q", r, old, cur)
		}
	})
}

// Source: the decision: a changed line means values changed. A change that
// only appends a comma is a moved separator, covered below.
func TestPropertyChangedLineIsValuesChanged(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		old := textLines(t, "old")
		if len(old) == 0 {
			old = []string{"a"}
		}
		at := rapid.IntRange(0, len(old)-1).Draw(t, "at")
		repl := rapid.StringMatching(`[abc]{1,4}`).Draw(t, "replacement")
		if repl == old[at] || repl == old[at]+"," {
			t.Skip("not a change")
		}
		cur := slices.Clone(old)
		cur[at] = repl
		if r := Compare(join(old), join(cur)); r.Label != ValuesChanged {
			t.Fatalf("got %+v, want values-changed\nold %q\ncur %q", r, old, cur)
		}
	})
}

func TestSeparatorMove(t *testing.T) {
	// Source: knowledge/decisions/snapshot-diff-labels.md. Pretty-printed
	// JSON in a transcript puts one field per line, so a field added after
	// the last one puts a comma on the old last line. The decision counts
	// that as additive.
	accepted := "{\n  \"a\": 1,\n  \"b\": 2\n}\n"
	current := "{\n  \"a\": 1,\n  \"b\": 2,\n  \"c\": 3\n}\n"
	r := Compare(accepted, current)
	if r.Label != AdditiveOnly || r.Added != 1 || r.Removed != 0 {
		t.Fatalf("got %+v", r)
	}
	if !strings.Contains(r.Diff, "~  \"b\": 2,") || !strings.Contains(r.Diff, "+  \"c\": 3") {
		t.Errorf("diff = %q", r.Diff)
	}

	// A value that changes along with its comma is still a change.
	r = Compare(accepted, "{\n  \"a\": 1,\n  \"b\": 9,\n  \"c\": 3\n}\n")
	if r.Label != ValuesChanged {
		t.Fatalf("changed value: got %+v", r)
	}

	// A comma line elsewhere does not excuse a removal.
	r = Compare("x\ny\nz\n", "x,\nz\ny,\n")
	if r.Label != ValuesChanged {
		t.Fatalf("pairing across changes: got %+v", r)
	}
}

func TestDiffRendering(t *testing.T) {
	old := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	cur := slices.Clone(old)
	cur[1] = "two"
	cur[8] = "nine"
	r := Compare(join(old), join(cur))
	want := "@@ line 1\n 1\n-2\n+two\n 3\n 4\n@@ line 7\n 7\n 8\n-9\n+nine\n 10"
	if r.Diff != want {
		t.Fatalf("diff =\n%s\nwant\n%s", r.Diff, want)
	}
}

func TestCRLFAndTrailingNewline(t *testing.T) {
	if r := Compare("a\r\nb\r\n", "a\nb"); r.Label != "" {
		t.Fatalf("line endings counted as a change: %+v", r)
	}
	if r := Compare("", "a\n"); r.Label != AdditiveOnly {
		t.Fatalf("new file: %+v", r)
	}
}

func TestByCountFallback(t *testing.T) {
	ops := byCount([]string{"a", "b", "a"}, []string{"a", "c", "b"})
	var dels, inss []string
	for _, o := range ops {
		switch o.kind {
		case del:
			dels = append(dels, o.text)
		case ins:
			inss = append(inss, o.text)
		}
	}
	if !slices.Equal(dels, []string{"a"}) || !slices.Equal(inss, []string{"c"}) {
		t.Fatalf("dels %q ins %q", dels, inss)
	}
}

func TestDescribe(t *testing.T) {
	for _, l := range []string{AdditiveOnly, ValuesChanged} {
		if d := Describe(l); d == l || d == "" {
			t.Errorf("Describe(%q) = %q", l, d)
		}
	}
}
