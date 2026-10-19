package summary

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/approval"
	"github.com/hbaldwin98/reliab/internal/gate"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/verify"
)

// The layout rules of knowledge/decisions/pr-evidence-summary.md, on
// summaries built by hand: what appears when, and how each line reads.
func TestMarkdownLayout(t *testing.T) {
	bare := func() *Summary {
		return &Summary{Tier: "prepr", Profile: "p", Tool: "reliab dev", StartedAt: "2026-10-18T10:00:00Z",
			Complete: true, Fresh: true, TreeHash: strings.Repeat("a", 40), CurrentTree: strings.Repeat("a", 40)}
	}
	tests := []struct {
		name      string
		edit      func(s *Summary)
		want, not []string
	}{
		{"empty sections", func(*Summary) {}, []string{
			"| (none) | | | |",
			"- Open: 0. Acknowledged by a person: 0. Fixed during the change: 0.",
			"#### Questions for a human\n\nNone: no open finding or protected-path change waits for a person.",
			"- Tree `aaaaaaaaaaaa`.\n",
		}, []string{"#### Skipped", "#### Mutation", "Open:\n", "Acknowledged:\n", "Fixed during the change:\n",
			"not re-checked", "Fixed means", "at commit", "seed", "property cases", "Stale", "Freshness unknown", "Incomplete", "Protected changes a person approved", "Protected paths not checked"}},
		{"run details", func(s *Summary) {
			s.Head, s.Seed, s.PropertyCases, s.BudgetSec = strings.Repeat("b", 40), "42", 1000, 900
		}, []string{"- Tree `aaaaaaaaaaaa` at commit `bbbbbbbbbbbb`; seed 42, 1000 property cases.", "of a 900s budget"}, nil},
		{"no budget", func(s *Summary) { s.BudgetSec = 0 }, []string{"of a unlimited budget"}, []string{"0s budget"}},
		{"freshness unknown", func(s *Summary) { s.FreshError = "profile: bad\nyaml" }, []string{"> **Freshness unknown:** profile: bad yaml"}, []string{"Stale"}},
		{"stale", func(s *Summary) { s.Fresh, s.CurrentTree = false, strings.Repeat("c", 40) }, []string{
			"> **Stale:** the working tree changed after this report ran. It describes tree `aaaaaaaaaaaa`; the tree is now `cccccccccccc`."}, nil},
		{"incomplete", func(s *Summary) { s.Complete, s.BudgetSec = false, 300 }, []string{
			"> **Incomplete:** the 300s budget ran out", "of a 300s budget, incomplete."}, nil},
		{"skips only", func(s *Summary) { s.Skipped = []report.Skip{{ID: "corpus", Reason: report.SkipNoCommand}} },
			[]string{"#### Skipped\n\n| Command | Why | Detail |\n|---|---|---|\n| corpus | no command in the profile |  |\n\n####"}, []string{"Not in the"}},
		// knowledge/decisions/schema-v0-shape.md: a needed but missing role
		// reads differently from one that does not apply.
		{"role missing", func(s *Summary) {
			s.Skipped = []report.Skip{{ID: "properties", Reason: report.SkipRoleMissing, Detail: "T2: no property library yet"}}
		}, []string{"| properties | role needed but missing | T2: no property library yet |"}, []string{"role not applicable"}},
		{"not in tier only", func(s *Summary) { s.NotInTier = []string{"mutation_full"} },
			[]string{"#### Skipped\n\nNot in the prepr tier: mutation_full.\n"}, []string{"| Command | Why |"}},
		{"skips and not in tier", func(s *Summary) {
			s.Skipped = []report.Skip{{ID: "unit", Reason: report.SkipBudget, Detail: "killed after 300.0s"}}
			s.NotInTier = []string{"mutation_full"}
		}, []string{"| unit | budget ran out | killed after 300.0s |\n\nNot in the prepr tier: mutation_full."}, nil},
		{"mutation counts", func(s *Summary) {
			s.Mutation = []Mutation{
				{Command: "mutation_changed", Mutants: &report.MutantCounts{Killed: 5}, Survivors: []Item{}},
				{Command: "mutation_full", Mutants: &report.MutantCounts{Killed: 5, Survived: 1, NotCovered: 2}, Survivors: []Item{
					{ID: "F-m", Kind: "unclassified", Status: "open", Subject: "a.go: X", Location: "a.go:1:2"}}},
				{Command: "mutation_other", Survivors: []Item{}},
			}
		}, []string{
			"- `mutation_changed`: 5 tested, 5 killed, 0 survived.\n",
			"- `mutation_full`: 6 tested, 5 killed, 1 survived; 2 not covered by any test.\n",
			"- `mutation_other`: the tool reported no counts; 0 survivor finding(s).",
			"| `F-m` a.go: X | a.go:1:2 | unclassified | open |  |",
		}, nil},
		{"mutation without survivors", func(s *Summary) {
			s.Mutation = []Mutation{{Command: "mutation_changed", Mutants: &report.MutantCounts{Killed: 3}, Survivors: []Item{}}}
		}, []string{"#### Mutation"}, []string{"| Survivor |"}},
		{"subjects", func(s *Summary) {
			s.Open = []Item{
				{ID: "F-build", Kind: "unclassified", Status: "open", Command: "build"},
				{ID: "F-snap", Kind: "unclassified", Status: "open", Command: "snapshots", Subject: "snapshots/a.snap", Location: "snapshots/a.snap", Diff: "additive-only"},
				{ID: "F-unit", Kind: "new-code-defect", Status: "open", Command: "unit", Subject: "TestX", Location: "x_test.go:3", Reason: "a | b"},
			}
		}, []string{
			"| `F-build` | unclassified | command build |  |",
			"| `F-snap` | unclassified | snapshots/a.snap (diff: additive-only) |",
			"| `F-unit` | new-code-defect | TestX at x_test.go:3 | a \\| b |",
		}, nil},
		{"acknowledged", func(s *Summary) {
			s.Acknowledged = []Item{{ID: "F-q", Kind: "legacy-defect-or-quirk", Status: "acknowledged", Subject: "TestOld", Reason: "predates the change", AckBy: "human:owner"}}
		}, []string{"Acknowledged:\n\n| Finding | Kind | Subject | Reason |", "| predates the change (acknowledged by human:owner) |"}, nil},
		{"base and earlier runs", func(s *Summary) {
			s.Base = "main"
			s.NotRechecked = []Item{{ID: "F-f", Kind: "unclassified", Command: "mutation_full", Subject: "b.go: Y", SeenIn: "on-demand 2026-10-18T09:00:00Z"}}
		}, []string{"since its merge base with `main`", "commands this report did not run (1), so not re-checked:", "| on-demand 2026-10-18T09:00:00Z |"}, nil},
		{"questions", func(s *Summary) {
			s.Questions = []Item{
				{ID: "F-a", Kind: report.KindEnvironment, Command: "unit", Ask: asks[report.KindEnvironment], Reason: "out of disk"},
				{ID: "F-b", Kind: report.KindEquivalentMutant, Command: "mutation_changed", Subject: "c.go: Z", Ask: asks[report.KindEquivalentMutant]},
				{ID: "F-c", Kind: report.KindLegacyDefectOrQuirk, Command: "unit", Subject: "TestOld", Ask: asks[report.KindLegacyDefectOrQuirk], Reason: "fails on main"},
				{ID: "F-d", Kind: report.KindCharacterizationDiff, Command: "snapshots", Subject: "s.snap", Ask: asks[report.KindCharacterizationDiff], Reason: "new field"},
			}
		}, []string{
			"1. `F-a` [environment] command unit: fix the machine, then re-run. Recorded reason: \"out of disk\"\n",
			"2. `F-b` [equivalent-mutant] c.go: Z: confirm that no test can kill this mutant\n",
			"3. `F-c` [legacy-defect-or-quirk] TestOld: keep this pre-existing behavior or have it fixed. Recorded reason:",
			"4. `F-d` [characterization-diff] s.snap: approve the changed output or revert the change. Recorded reason:",
		}, []string{"None:", "?."}},
		// knowledge/decisions/protected-path-approval.md: a protected change
		// without a person's approval is a question for a human, numbered
		// after the findings; an approved one names who approved it.
		{"protected changes", func(s *Summary) {
			s.Base = "main"
			s.Questions = []Item{{ID: "F-a", Kind: report.KindEnvironment, Command: "unit", Ask: asks[report.KindEnvironment]}}
			s.Protected = []gate.ProtectedChange{
				{Path: "testdata/seeds/x", Glob: "testdata/**"},
				{Path: "snapshots/a.snap", Glob: "snapshots/**", Approval: &approval.Approval{By: "human:owner", At: "2026-10-18T11:00:00Z"}},
				{Path: "testdata/seeds/y", Glob: "testdata/**"},
			}
		}, []string{
			"2. `testdata/seeds/x` (protected by `testdata/**`) changed since `main` without a person's approval: approve this content with `reliab approve testdata/seeds/x --by human:<name>`, or have the change reverted (R2)\n",
			"\n3. `testdata/seeds/y` (protected by `testdata/**`)",
			"Protected changes a person approved (a local record; CI and the code host's review still decide):\n\n- `snapshots/a.snap` (protected by `snapshots/**`): approved by human:owner at 2026-10-18T11:00:00Z.\n",
		}, []string{"None:", "`snapshots/a.snap` (protected by `snapshots/**`) changed since"}},
		{"only approved protected changes", func(s *Summary) {
			s.Protected = []gate.ProtectedChange{{Path: "snapshots/a.snap", Glob: "snapshots/**", Approval: &approval.Approval{By: "human:owner"}}}
		}, []string{"None: no open finding or protected-path change waits for a person.", "approved by human:owner"}, []string{"1. "}},
		{"only unapproved protected changes", func(s *Summary) {
			s.Base = "main"
			s.Protected = []gate.ProtectedChange{{Path: "snapshots/a.snap", Glob: "snapshots/**"}}
		}, []string{"1. `snapshots/a.snap` (protected by `snapshots/**`) changed since `main`"}, []string{"None:", "Protected changes a person approved"}},
		{"protected paths not checked", func(s *Summary) { s.ProtectedError = "no base ref\nfound" },
			[]string{"- Protected paths not checked: no base ref found."}, []string{"None:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := bare()
			tt.edit(s)
			md := Markdown(s)
			for _, w := range tt.want {
				if !strings.Contains(md, w) {
					t.Errorf("lacks %q:\n%s", w, md)
				}
			}
			for _, n := range tt.not {
				if strings.Contains(md, n) {
					t.Errorf("has %q:\n%s", n, md)
				}
			}
		})
	}
}

// knowledge/decisions/pr-evidence-summary.md: the base defaults to the first
// of origin/HEAD, origin/main, main, master that resolves; with none, the
// summary says the earlier runs were not read. An earlier run of the same
// tier counts like any other.
func TestBaseAndEarlierRuns(t *testing.T) {
	root := testrepo.New(t, "commands:\n  build: \"true\"\n  unit:\n    run: cp test/u.xml .reliab/out/u.xml; exit 1\n    format: junit\n    output: .reliab/out/u.xml\n")
	at := time.Date(2036, 10, 18, 9, 0, 0, 0, time.UTC)
	for _, xml := range []string{twoFailures, oneFailure} {
		testrepo.Write(t, root, "test/u.xml", xml)
		at = at.Add(time.Minute)
		now := at
		if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr", Now: func() time.Time { return now }}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Build(Options{Root: root, Tier: "prepr"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Base != "main" || s.BaseError != "" {
		t.Errorf("base = %q (%s), want main", s.Base, s.BaseError)
	}
	if got := strings.Join(ids(s.Fixed), ","); got != "TestRetryBackoff" {
		t.Errorf("fixed = %s, want the failure the earlier prepr run had", got)
	}

	testrepo.Git(t, root, "branch", "-m", "main", "trunk")
	s, err = Build(Options{Root: root, Tier: "prepr"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Base != "" || !strings.Contains(s.BaseError, "no base ref found") || len(s.Fixed) != 0 {
		t.Errorf("no base: base %q, error %q, fixed %d", s.Base, s.BaseError, len(s.Fixed))
	}
	if s, err := Build(Options{Root: root, Tier: "prepr", Base: "trunk"}); err != nil || s.Base != "trunk" || len(s.Fixed) != 1 {
		t.Errorf("explicit base: %+v, %v", s, err)
	}
}

// A profile that cannot be read leaves freshness unknown, not assumed.
func TestFreshnessUnknown(t *testing.T) {
	root := change(t)
	testrepo.Write(t, root, "reliability-profile.yaml", "schemaVersion: 0.1.0\n")
	s := build(t, root)
	if s.Fresh || s.FreshError == "" {
		t.Fatalf("fresh=%t error=%q", s.Fresh, s.FreshError)
	}
	if md := Markdown(s); !strings.Contains(md, "> **Freshness unknown:**") {
		t.Errorf("markdown:\n%s", md)
	}
}
