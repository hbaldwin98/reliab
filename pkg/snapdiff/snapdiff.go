// Package snapdiff labels the difference between an accepted text snapshot
// and a new one, so a person reviewing it can tell a deliberate addition
// from a change (knowledge/decisions/snapshot-diff-labels.md). The label
// only helps a person decide; approval stays human (R2).
package snapdiff

import (
	"fmt"
	"strings"
)

// Labels.
const (
	// AdditiveOnly: lines were added and none removed or changed.
	AdditiveOnly = "additive-only"
	// ValuesChanged: some line was removed or changed.
	ValuesChanged = "values-changed"
)

// Describe says what a label means, for messages.
func Describe(label string) string {
	switch label {
	case AdditiveOnly:
		return "additive only: lines added, nothing removed or changed"
	case ValuesChanged:
		return "values changed or removed: review every - line"
	}
	return label
}

// Result is the difference between two texts.
type Result struct {
	Label          string // "" when the texts are equal
	Added, Removed int    // lines, after separator moves are paired
	Diff           string // changed lines with a little context
}

// context is how many unchanged lines the diff keeps around a change.
const context = 2

// maxCells bounds the line-matching table. Past it, lines are compared as
// counts, which still labels correctly except that a pure reordering reads
// as additive.
const maxCells = 1 << 24

// Compare labels the change from accepted to current. A line that only
// gains a trailing comma does not count as changed, because adding an item
// after the last one in a rendered list or object moves the separator onto
// the old last line.
func Compare(accepted, current string) Result {
	a, b := lines(accepted), lines(current)
	ops := diff(a, b)
	var removed, added []int
	for i, op := range ops {
		switch op.kind {
		case del:
			removed = append(removed, i)
		case ins:
			added = append(added, i)
		}
	}
	if len(removed) == 0 && len(added) == 0 {
		return Result{}
	}
	ops = pairSeparators(ops, removed, added)
	res := Result{Diff: render(ops)}
	for _, op := range ops {
		switch op.kind {
		case del:
			res.Removed++
		case ins:
			res.Added++
		}
	}
	res.Label = AdditiveOnly
	if res.Removed > 0 {
		res.Label = ValuesChanged
	}
	return res
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	// A final newline ends the last line; it does not start another.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

type kind int

const (
	same kind = iota
	del
	ins
	// moved is a removed line that came back with a trailing comma: the
	// separator moved, the value did not.
	moved
)

type op struct {
	kind kind
	text string
}

// pairSeparators turns each removed line that reappears in the same change
// with only a trailing comma added into a moved line, and drops the
// matching insert.
func pairSeparators(ops []op, removed, added []int) []op {
	// change[i] numbers the run of edits op i belongs to.
	change := make([]int, len(ops))
	n := 0
	for i, o := range ops {
		if o.kind == same {
			n++
		}
		change[i] = n
	}
	drop := map[int]bool{}
	for _, r := range removed {
		for _, a := range added {
			if !drop[a] && change[a] == change[r] && ops[a].text == ops[r].text+"," {
				drop[a] = true
				ops[r] = op{moved, ops[a].text}
				break
			}
		}
	}
	out := ops[:0:0]
	for i, o := range ops {
		if !drop[i] {
			out = append(out, o)
		}
	}
	return out
}

// diff returns the edit script from a to b, deletions before insertions
// within a change.
func diff(a, b []string) []op {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var out []op
	for _, s := range a[:pre] {
		out = append(out, op{same, s})
	}
	am, bm := a[pre:len(a)-suf], b[pre:len(b)-suf]
	if len(am)*len(bm) > maxCells {
		out = append(out, byCount(am, bm)...)
	} else {
		out = append(out, lcs(am, bm)...)
	}
	for _, s := range a[len(a)-suf:] {
		out = append(out, op{same, s})
	}
	return out
}

// lcs is the classic longest-common-subsequence edit script.
func lcs(a, b []string) []op {
	n, m := len(a), len(b)
	// t[i][j] is the LCS length of a[i:] and b[j:].
	t := make([][]int32, n+1)
	for i := range t {
		t[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i][j] = t[i+1][j+1] + 1
			} else {
				t[i][j] = max(t[i+1][j], t[i][j+1])
			}
		}
	}
	var out []op
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			out = append(out, op{same, a[i]})
			i++
			j++
		case j == m || (i < n && t[i+1][j] >= t[i][j+1]):
			out = append(out, op{del, a[i]})
			i++
		default:
			out = append(out, op{ins, b[j]})
			j++
		}
	}
	return out
}

// byCount compares lines as multisets: a line is removed when it occurs
// fewer times in b than in a.
func byCount(a, b []string) []op {
	count := map[string]int{}
	for _, s := range b {
		count[s]++
	}
	var out []op
	for _, s := range a {
		if count[s] > 0 {
			count[s]--
			continue
		}
		out = append(out, op{del, s})
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		if seen[s] > 0 {
			seen[s]--
			continue
		}
		out = append(out, op{ins, s})
	}
	return out
}

// render writes changed lines with context, "..." between distant changes,
// and "~" for a line whose only change is a trailing comma.
func render(ops []op) string {
	keep := make([]bool, len(ops))
	for i, o := range ops {
		if o.kind == del || o.kind == ins || o.kind == moved {
			for k := max(0, i-context); k <= min(len(ops)-1, i+context); k++ {
				keep[k] = true
			}
		}
	}
	var b strings.Builder
	gap := false
	line := 0 // line number in the accepted text
	for i, o := range ops {
		if o.kind != ins {
			line++
		}
		if !keep[i] {
			gap = true
			continue
		}
		if gap || b.Len() == 0 {
			fmt.Fprintf(&b, "@@ line %d\n", max(line, 1))
			gap = false
		}
		b.WriteString(marks[o.kind] + o.text + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

var marks = map[kind]string{same: " ", del: "-", ins: "+", moved: "~"}
