package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

func feature(t *testing.T) (string, *profile.Profile) {
	t.Helper()
	root := testrepo.New(t, "protected_paths: [\"snapshots/**\"]\ncommands:\n  build: \"true\"\n")
	p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	return root, p
}

func commit(t *testing.T, root string) {
	t.Helper()
	testrepo.Git(t, root, "add", "-A")
	testrepo.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "change")
}

func TestProtected(t *testing.T) {
	root, p := feature(t)

	if got, err := Protected(root, p, "main"); err != nil || len(got) != 0 {
		t.Fatalf("no changes: %v %v", got, err)
	}

	testrepo.Write(t, root, "src/a/a.txt", "edited")
	testrepo.Write(t, root, "snapshots/s.txt", "approved?")
	commit(t, root)
	testrepo.Write(t, root, "snapshots/new.txt", "untracked")

	got, err := Protected(root, p, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "snapshots/new.txt" || got[1].Path != "snapshots/s.txt" {
		t.Fatalf("protected changes = %+v", got)
	}
	if len(Unapproved(got)) != 2 {
		t.Errorf("nothing approved yet, but Unapproved = %+v", Unapproved(got))
	}
}

// knowledge/decisions/protected-path-approval.md: a person's approval
// covers the content they approved, so gate protected accepts that change,
// and any later edit to the file voids it.
func TestApprove(t *testing.T) {
	root, p := feature(t)
	testrepo.Write(t, root, "snapshots/seed.txt", "case 1")
	testrepo.Write(t, root, "snapshots/s.txt", "changed")
	commit(t, root)

	added, err := Approve(root, p, "main", []string{"snapshots/seed.txt"}, "human:pat", time.Now())
	if err != nil || len(added) != 1 || added[0].By != "human:pat" || added[0].Blob == "" || added[0].BaseBlob != "" || added[0].MergeBase == "" {
		t.Fatalf("approve: %+v, %v", added, err)
	}
	got, err := Protected(root, p, "main")
	if err != nil {
		t.Fatal(err)
	}
	un := Unapproved(got)
	if len(un) != 1 || un[0].Path != "snapshots/s.txt" {
		t.Fatalf("unapproved = %+v, want only snapshots/s.txt", un)
	}
	for _, c := range got {
		if c.Path == "snapshots/seed.txt" && (c.Approval == nil || c.Approval.By != "human:pat") {
			t.Errorf("seed approval = %+v", c.Approval)
		}
	}

	testrepo.Write(t, root, "snapshots/seed.txt", "case 1, edited after approval")
	if got, _ := Protected(root, p, "main"); len(Unapproved(got)) != 2 {
		t.Errorf("an edit after approval kept it: %+v", Unapproved(got))
	}
	testrepo.Write(t, root, "snapshots/seed.txt", "case 1")
	if got, _ := Protected(root, p, "main"); len(Unapproved(got)) != 1 {
		t.Errorf("the approved content again is approved again: %+v", Unapproved(got))
	}

	if err := os.Remove(filepath.Join(root, "snapshots", "seed.txt")); err != nil {
		t.Fatal(err)
	}
	if got, _ := Protected(root, p, "main"); len(Unapproved(got)) != 1 {
		t.Errorf("deleting an added file leaves no change, so nothing new to approve: %+v", Unapproved(got))
	}
	if err := os.Remove(filepath.Join(root, "snapshots", "s.txt")); err != nil {
		t.Fatal(err)
	}
	if got, _ := Protected(root, p, "main"); len(Unapproved(got)) != 1 || Unapproved(got)[0].Path != "snapshots/s.txt" {
		t.Errorf("a deletion is a change to approve: %+v", got)
	}
	if _, err := Approve(root, p, "main", []string{"snapshots/s.txt"}, "human:pat", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := Protected(root, p, "main"); len(Unapproved(got)) != 0 {
		t.Errorf("an approved deletion: %+v", Unapproved(got))
	}
}

func TestApproveRefusals(t *testing.T) {
	root, p := feature(t)
	testrepo.Write(t, root, "snapshots/s.txt", "changed")
	testrepo.Write(t, root, "src/a/a.txt", "changed")
	commit(t, root)
	for _, tt := range []struct {
		paths []string
		by    string
		want  string
	}{
		{[]string{"snapshots/s.txt"}, "reliab:reviewer", "human:<name>"},
		{[]string{"snapshots/s.txt"}, "human: ", "human:<name>"},
		{nil, "human:pat", "name the protected files"},
		{[]string{"src/a/a.txt"}, "human:pat", "not a protected file changed"},
		{[]string{"snapshots/unchanged.txt"}, "human:pat", "not a protected file changed"},
	} {
		if _, err := Approve(root, p, "main", tt.paths, tt.by, time.Now()); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Approve(%v, %q) = %v, want an error with %q", tt.paths, tt.by, err, tt.want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".reliab", "approvals.json")); !os.IsNotExist(err) {
		t.Errorf("a refused approval wrote the file: %v", err)
	}
}

// knowledge/decisions/mutation-threshold.md: the mutation baseline is a
// protected path, so a person approves a change to it like any other
// (knowledge/decisions/protected-path-approval.md).
func TestApproveMutationBaseline(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\"reliab/mutation-baseline.json\"]\nmutation_baseline: reliab/mutation-baseline.json\ncommands:\n  build: \"true\"\n")
	p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, p.MutationBaseline, `{"schemaVersion":"0.1.0","scopes":{}}`)
	commit(t, root)
	if got, err := Protected(root, p, "main"); err != nil || len(Unapproved(got)) != 1 || got[0].Path != p.MutationBaseline {
		t.Fatalf("baseline change: %+v, %v", got, err)
	}
	if _, err := Approve(root, p, "main", []string{p.MutationBaseline}, "human:owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := Protected(root, p, "main"); len(Unapproved(got)) != 0 {
		t.Errorf("approved baseline still unapproved: %+v", got)
	}
}

// knowledge/decisions/protected-path-approval.md: the profile guards itself,
// and a branch that drops a glob from protected_paths still has its changes
// under it listed: the base profile's globs count too.
func TestProtectedProfile(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\"snapshots/**\", \"reliability-profile.yaml\"]\ncommands:\n  build: \"true\"\n")
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, "reliability-profile.yaml", strings.Replace(readFile(t, root, "reliability-profile.yaml"),
		"protected_paths: [\"snapshots/**\", \"reliability-profile.yaml\"]", "protected_paths: []", 1))
	testrepo.Write(t, root, "snapshots/s.txt", "rewritten")
	p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Protected(root, p, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "reliability-profile.yaml" || got[1].Path != "snapshots/s.txt" || got[1].Glob != "snapshots/**" {
		t.Fatalf("protected changes = %+v, want the profile and the snapshot it stopped protecting", got)
	}
	if _, err := Approve(root, p, "main", []string{"reliability-profile.yaml"}, "human:pat", time.Now()); err != nil {
		t.Errorf("a person approves a profile change: %v", err)
	}

	testrepo.Git(t, root, "checkout", "-q", "-f", "main")
	testrepo.Write(t, root, "reliability-profile.yaml", "not: [a profile")
	commit(t, root)
	testrepo.Git(t, root, "checkout", "-q", "-b", "after")
	if _, err := Protected(root, p, "main"); err == nil || !strings.Contains(err.Error(), "merge base") {
		t.Errorf("a base profile that does not load: err = %v, want it named", err)
	}
}
