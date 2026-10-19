package gitx

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// One base for every changed-code question: a flag, then the profile, then
// the first fallback with a merge base.
func TestResolveBase(t *testing.T) {
	dir := newRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "topic")
	for _, c := range []struct{ explicit, profile, want string }{
		{"", "", "main"},
		{"", "topic", "topic"},
		{"main", "topic", "main"},
	} {
		got, err := ResolveBase(dir, c.explicit, c.profile)
		if err != nil || got != c.want {
			t.Errorf("ResolveBase(%q, %q) = %q, %v; want %q", c.explicit, c.profile, got, err, c.want)
		}
	}
	if _, err := ResolveBase(dir, "", "nosuch"); err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("unknown profile base: %v", err)
	}
	gitIn(t, dir, "branch", "-q", "-m", "main", "trunk")
	if _, err := ResolveBase(dir, "", ""); err == nil || !strings.Contains(err.Error(), "origin/HEAD") {
		t.Errorf("no fallback: %v", err)
	}
}

// Changed lines since the merge base: committed and uncommitted edits on
// the branch, and untracked files whole; not what main did after the fork.
func TestChangedLines(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "b.txt", "1\n2\n3\n4\n")
	write(t, dir, "gone.txt", "1\n2\n")
	write(t, dir, "old/moved.txt", "a\nb\nc\nd\ne\nf\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-qm", "b")
	gitIn(t, dir, "checkout", "-q", "-b", "topic")
	write(t, dir, "b.txt", "1\ntwo\n3\n4\nfive\n")
	gitIn(t, dir, "rm", "-q", "gone.txt")
	// A move without edits is not a change (the move to pkg/ once made
	// whole packages look changed).
	gitIn(t, dir, "mv", "old/moved.txt", "moved.txt")
	gitIn(t, dir, "commit", "-qam", "edit")
	write(t, dir, "b.txt", "1\ntwo\n3\nfour\nfive\n")
	write(t, dir, "new.txt", "x\ny\n")
	write(t, dir, "no-newline.txt", "p\nq")
	write(t, dir, "empty.txt", "")
	write(t, dir, "ignored/skip.txt", "z\n")
	got, err := ChangedLines(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[int]bool{
		"b.txt":          {2: true, 4: true, 5: true},
		"new.txt":        {1: true, 2: true},
		"no-newline.txt": {1: true, 2: true},
		"empty.txt":      {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ChangedLines = %v, want %v", got, want)
	}
}
