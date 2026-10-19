package report

import (
	"testing"

	"pgregory.net/rapid"
)

// The score is killed over counted, in percent, and nothing counted is
// never below anything (knowledge/decisions/mutation-threshold.md).
func TestPercentAndBelow(t *testing.T) {
	if got := Percent(7, 10); got != 70 {
		t.Errorf("Percent(7, 10) = %v", got)
	}
	if got := Percent(0, 0); got != 0 {
		t.Errorf("Percent(0, 0) = %v", got)
	}
	if !Below(7, 10, 80) || Below(8, 10, 80) || Below(9, 10, 80) || Below(0, 0, 80) {
		t.Error("Below: 7 of 10 is below 80%; 8 and 9 of 10 are not; nothing counted is never below")
	}
	rapid.Check(t, func(t *rapid.T) {
		counted := rapid.IntRange(1, 1000).Draw(t, "counted")
		killed := rapid.IntRange(0, counted).Draw(t, "killed")
		want := float64(rapid.IntRange(0, 100).Draw(t, "want"))
		// Integer cross-multiplication is the exact comparison.
		if got, exact := Below(killed, counted, want), 100*killed < int(want)*counted; got != exact {
			t.Fatalf("Below(%d, %d, %v) = %v, want %v", killed, counted, want, got, exact)
		}
		if p := Percent(killed, counted); p < 0 || p > 100 {
			t.Fatalf("Percent(%d, %d) = %v", killed, counted, p)
		}
	})
}
