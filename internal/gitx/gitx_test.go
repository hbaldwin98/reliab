package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// newRepo creates a git repository with one committed file.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.invalid"},
		{"config", "user.name", "t"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write(t, dir, "a.txt", "a")
	write(t, dir, ".gitignore", "ignored/\n")
	if out, err := exec.Command("git", "-C", dir, "add", "-A").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if out, err := exec.Command("git", "-C", dir, "commit", "-qm", "init").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	return dir
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hash(t *testing.T, dir string, exclude ...string) string {
	t.Helper()
	h, err := TreeHash(dir, exclude)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestTreeHash(t *testing.T) {
	dir := newRepo(t)
	base := hash(t, dir)
	if len(base) != 40 && len(base) != 64 {
		t.Fatalf("hash %q is not a git object id", base)
	}
	if got := hash(t, dir); got != base {
		t.Fatal("hash is not stable")
	}

	write(t, dir, "ignored/x.txt", "x")
	if hash(t, dir) != base {
		t.Error("an ignored file changed the hash")
	}

	write(t, dir, "docs/notes.md", "n")
	withDoc := hash(t, dir)
	if withDoc == base {
		t.Error("an untracked file did not change the hash")
	}
	if hash(t, dir, "docs/**") != base {
		t.Error("an excluded file changed the hash")
	}

	write(t, dir, "a.txt", "b")
	if hash(t, dir) == withDoc {
		t.Error("an edit did not change the hash")
	}

	out, err := exec.Command("git", "-C", dir, "diff", "--cached", "--name-only").Output()
	if err != nil || len(out) != 0 {
		t.Errorf("the real index was touched: %q %v", out, err)
	}
}

// A same-size edit in the second the index was written leaves the file's
// stat data matching its index entry; git re-reads such a "racily clean"
// entry only because its mtime is not older than the index file's own. The
// hash backs report freshness (knowledge/decisions/report-freshness.md), so
// missing this edit would let a stale report pass as fresh.
func TestTreeHashSeesASameSizeEditInTheIndexSecond(t *testing.T) {
	dir := newRepo(t)
	index := filepath.Join(dir, ".git", "index")
	second := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(filepath.Join(dir, "a.txt"), second, second); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "update-index", "--refresh").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if err := os.Chtimes(index, second, second); err != nil {
		t.Fatal(err)
	}
	before := hash(t, dir)

	write(t, dir, "a.txt", "b")
	if err := os.Chtimes(filepath.Join(dir, "a.txt"), second, second); err != nil {
		t.Fatal(err)
	}
	if hash(t, dir) == before {
		t.Fatal("a same-size edit in the index's second did not change the hash")
	}
}

func TestChangedFiles(t *testing.T) {
	dir := newRepo(t)
	if got, _ := ChangedFiles(dir); len(got) != 0 {
		t.Fatalf("clean repo reports changes: %v", got)
	}
	write(t, dir, "a.txt", "b")
	write(t, dir, "src/new.go", "package x")
	write(t, dir, "ignored/x.txt", "x")
	got, err := ChangedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.txt", "src/new.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("changed = %v, want %v", got, want)
	}
}

// Files lists what an on-demand --scope covers
// (knowledge/decisions/on-demand-verify.md): tracked and untracked files
// under the paths, never ignored ones.
func TestFiles(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "modified") // tracked and changed: listed once
	write(t, dir, "src/new.go", "package x")
	write(t, dir, "src/sub/b.go", "package sub")
	write(t, dir, "ignored/x.txt", "x")
	tests := []struct {
		paths []string
		want  []string
	}{
		{[]string{"src"}, []string{"src/new.go", "src/sub/b.go"}},
		{[]string{"a.txt", "src/sub"}, []string{"a.txt", "src/sub/b.go"}},
		{[]string{"."}, []string{".gitignore", "a.txt", "src/new.go", "src/sub/b.go"}},
		{[]string{"ignored"}, []string{}},
	}
	for _, tt := range tests {
		got, err := Files(dir, tt.paths)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Files(%v) = %v, want %v", tt.paths, got, tt.want)
		}
	}
	if _, err := Files(t.TempDir(), []string{"."}); err == nil {
		t.Error("no error outside a repository")
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// The summary ties history reports to a branch by their HEAD
// (knowledge/decisions/pr-evidence-summary.md), and the verdict checks the
// reviewer's worktree belongs to the same repository
// (knowledge/decisions/reviewer-verdict.md).
func TestHeadAncestryAndCommonDir(t *testing.T) {
	empty := t.TempDir()
	gitRun(t, empty, "init", "-q", "-b", "main")
	// The report schema: head is absent in a repository with no commits.
	if h := Head(empty); h != "" {
		t.Errorf("head with no commits = %q", h)
	}
	if _, err := MergeBase(empty, "main"); err == nil {
		t.Error("merge base with no commits: want an error")
	}

	dir := newRepo(t)
	base := Head(dir)
	if len(base) != 40 && len(base) != 64 {
		t.Fatalf("head = %q", base)
	}
	gitRun(t, dir, "checkout", "-q", "-b", "topic")
	write(t, dir, "b.txt", "b")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "topic")
	tip := Head(dir)
	if tip == base {
		t.Fatal("head did not move")
	}
	if mb, err := MergeBase(dir, "main"); err != nil || mb != base {
		t.Errorf("merge base = %q, %v; want %s", mb, err, base)
	}
	if !IsAncestor(dir, base, tip) || !IsAncestor(dir, tip, tip) {
		t.Error("base and tip are ancestors of tip")
	}
	if IsAncestor(dir, tip, base) {
		t.Error("tip is not an ancestor of base")
	}

	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, dir, "worktree", "add", "-q", "--detach", wt, "main")
	a, errA := CommonDir(dir)
	b, errB := CommonDir(wt)
	if errA != nil || errB != nil || a != b {
		t.Errorf("common dirs %q (%v) and %q (%v) differ", a, errA, b, errB)
	}
	if other, _ := CommonDir(empty); other == a {
		t.Error("another repository shares the common dir")
	}
	if _, err := CommonDir(t.TempDir()); err == nil {
		t.Error("common dir outside a repository: want an error")
	}
}

// internal/gate: an approval names the content it approved by blob hash, on
// the base and in the working tree, so any edit after it voids it.
func TestBlobs(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "x.txt", "one")
	gitRun(t, dir, "add", "x.txt")
	gitRun(t, dir, "commit", "-qm", "x")
	head := Head(dir)
	committed := BlobAt(dir, head, "x.txt")
	if w, err := WorkingBlob(dir, "x.txt"); err != nil || w != committed || committed == "" {
		t.Fatalf("unchanged file: working %q (%v), committed %q", w, err, committed)
	}
	write(t, dir, "x.txt", "two")
	if w, _ := WorkingBlob(dir, "x.txt"); w == committed {
		t.Error("an edit kept the blob hash")
	}
	if w, err := WorkingBlob(dir, "gone.txt"); err != nil || w != "" {
		t.Errorf("missing file: %q, %v", w, err)
	}
	if b := BlobAt(dir, head, "gone.txt"); b != "" {
		t.Errorf("missing in commit: %q", b)
	}
}

// internal/gate: gate protected reads the profile on the merge base, so a
// branch that drops a protected glob still has its changes listed
// (knowledge/decisions/protected-path-approval.md).
func TestFileAt(t *testing.T) {
	dir := newRepo(t)
	head := Head(dir)
	write(t, dir, "a.txt", "edited")
	if b, err := FileAt(dir, head, "a.txt"); err != nil || string(b) != "a" {
		t.Errorf("FileAt = %q, %v; want the committed content", b, err)
	}
	if _, err := FileAt(dir, head, "gone.txt"); err == nil {
		t.Error("a file the commit lacks: want an error")
	}
}
