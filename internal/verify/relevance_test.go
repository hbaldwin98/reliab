package verify

import (
	"context"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// Every rule tested here is stated in knowledge/decisions/relevance-skips.md.

const relevant = `commands:
  build: "true"
  faults_db:
    run: "true"
    relevant_when: ["src/data/**", "**/*Repository*"]
  unit: "true"
tiers:
  inner: { run: [build, faults_db, unit] }
  prepr: { run: [build, faults_db, unit] }
  nightly: { run: [build, faults_db, unit] }
`

func ran(rep *report.Report, id string) *report.CommandResult {
	for i := range rep.Run.Commands {
		if rep.Run.Commands[i].ID == id {
			return &rep.Run.Commands[i]
		}
	}
	return nil
}

func run(t *testing.T, opt Options) *report.Report {
	t.Helper()
	opt.Profile = "reliability-profile.yaml"
	rep, _, err := Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func feature(t *testing.T) string {
	t.Helper()
	root := repo(t, relevant)
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	return root
}

func TestRelevanceSkipsAnUnrelatedChange(t *testing.T) {
	root := feature(t)
	testrepo.Write(t, root, "docs/guide.md", "words")
	testrepo.Write(t, root, "src/a/a.txt", "edited")
	for _, tier := range []string{"inner", "prepr"} {
		rep := run(t, Options{Root: root, Tier: tier})
		s := skip(rep, "faults_db")
		if s == nil || s.Reason != report.SkipNotRelevant {
			t.Fatalf("%s: faults_db skip = %+v, want not-relevant", tier, s)
		}
		want := "the change since main touches only docs/guide.md, src/a/a.txt; it is relevant only to src/data/**, **/*Repository*"
		if s.Detail != want {
			t.Errorf("%s: detail = %q, want %q", tier, s.Detail, want)
		}
		if c := ran(rep, "unit"); c == nil || c.Relevance != "" {
			t.Errorf("%s: unit, with no relevance rule, = %+v", tier, c)
		}
		if rep.Run.Base != "main" || !rep.Run.Complete || rep.HasOpen() {
			t.Errorf("%s: run = %+v", tier, rep.Run)
		}
	}
}

func TestRelevanceRunsWhatTheChangeTouches(t *testing.T) {
	root := feature(t)
	testrepo.Write(t, root, "src/orders/OrderRepository.cs", "class R {}")
	testrepo.Git(t, root, "add", "-A")
	testrepo.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "repo")
	rep := run(t, Options{Root: root, Tier: "prepr"})
	c := ran(rep, "faults_db")
	if c == nil || c.Relevance != "src/orders/OrderRepository.cs changed since main and matches **/*Repository*" {
		t.Fatalf("faults_db = %+v; a committed change on the branch counts", c)
	}
	if skip(rep, "faults_db") != nil {
		t.Error("faults_db both ran and was skipped")
	}
}

func TestRelevanceNeverSkipsNightlyOrOnlyOrWithoutABase(t *testing.T) {
	root := feature(t)
	testrepo.Write(t, root, "docs/guide.md", "words")
	for name, tt := range map[string]struct {
		opt  Options
		want string
	}{
		"nightly":  {Options{Tier: "nightly"}, "the nightly tier never skips a command as not relevant"},
		"--only":   {Options{Tier: "prepr", Only: []string{"faults_db"}}, "asked for by --only, which never skips a command as not relevant"},
		"no base":  {Options{Tier: "prepr", Base: "no-such-ref"}, "relevance not judged, so it ran: no merge base with no-such-ref"},
		"--only 2": {Options{Tier: "inner", Only: []string{"build", "faults_db"}}, "asked for by --only"},
	} {
		tt.opt.Root = root
		rep := run(t, tt.opt)
		c := ran(rep, "faults_db")
		if c == nil || !strings.HasPrefix(c.Relevance, tt.want) || skip(rep, "faults_db") != nil {
			t.Errorf("%s: faults_db = %+v, skipped %+v; want it run because %q", name, c, skip(rep, "faults_db"), tt.want)
		}
	}
}

func TestRelevanceWithNothingChanged(t *testing.T) {
	root := feature(t)
	rep := run(t, Options{Root: root, Tier: "inner"})
	if s := skip(rep, "faults_db"); s == nil || s.Detail != "nothing changed since main, and it is relevant only to src/data/**, **/*Repository*" {
		t.Errorf("skip = %+v", s)
	}
}

// A run whose commands declare no relevance resolves no base for it.
func TestNoRelevanceRulesNeedNoBase(t *testing.T) {
	root := repo(t, "commands:\n  build: \"true\"\n")
	testrepo.Git(t, root, "branch", "-m", "main", "trunk")
	rep := run(t, Options{Root: root, Tier: "prepr"})
	if rep.Run.Base != "" || len(rep.Run.Commands) != 1 {
		t.Errorf("run = %+v", rep.Run)
	}
}

func TestFew(t *testing.T) {
	if got := few([]string{"a", "b", "c", "d", "e"}, 3); got != "a, b, c and 2 more" {
		t.Errorf("few = %q", got)
	}
	if got := few([]string{"a", "b", "c"}, 3); got != "a, b, c" {
		t.Errorf("few = %q", got)
	}
}

// The reason names the matching file that most likely made the command
// relevant: a product file before a test file, a test file before anything
// else, whatever their order. A test seed listed first once hid the
// product change behind it.
func TestRelevanceReasonPrefersProductThenTests(t *testing.T) {
	root := feature(t)
	testrepo.Write(t, root, "a/OrderRepository.seed", "seed")
	testrepo.Write(t, root, "test/OrderRepositoryTest.cs", "class T {}")
	if c := ran(run(t, Options{Root: root, Tier: "prepr"}), "faults_db"); c == nil ||
		c.Relevance != "test/OrderRepositoryTest.cs changed since main and matches **/*Repository*" {
		t.Errorf("test over other: %+v", c)
	}
	testrepo.Write(t, root, "src/orders/OrderRepository.cs", "class R {}")
	if c := ran(run(t, Options{Root: root, Tier: "prepr"}), "faults_db"); c == nil ||
		c.Relevance != "src/orders/OrderRepository.cs changed since main and matches **/*Repository*" {
		t.Errorf("product over test and other: %+v", c)
	}
}
