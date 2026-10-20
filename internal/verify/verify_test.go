package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

func repo(t *testing.T, extra string) string { return testrepo.New(t, extra) }

func verify(t *testing.T, root, tier string) *report.Report {
	t.Helper()
	rep, path, err := Run(context.Background(), Options{Root: root, Profile: "reliability-profile.yaml", Tier: tier})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := report.Read(path); err != nil {
		t.Fatalf("written report does not read back: %v", err)
	}
	return rep
}

func skip(rep *report.Report, id string) *report.Skip {
	for i := range rep.Run.Skipped {
		if rep.Run.Skipped[i].ID == id {
			return &rep.Run.Skipped[i]
		}
	}
	return nil
}

func TestVerifyRunsCommandsAndReportsFindings(t *testing.T) {
	root := repo(t, `commands:
  build: echo building
  analyzers: echo lint error >&2; exit 3
  unit:
    run: printf '<testsuite><testcase name="t1"><failure>boom</failure></testcase><testcase name="t2"/></testsuite>' > .reliab/out/u.xml; exit 1
    format: junit
    output: .reliab/out/u.xml
`)
	rep := verify(t, root, "inner")

	if !rep.Run.Complete || len(rep.Run.Commands) != 3 {
		t.Fatalf("run = %+v", rep.Run)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %+v, want analyzers and t1", rep.Findings)
	}
	if f := rep.Findings[0]; f.ID != "F-analyzers" || !strings.Contains(f.Message, "lint error") {
		t.Errorf("analyzers finding = %+v", f)
	}
	if f := rep.Findings[1]; f.Test != "t1" || f.Kind != report.KindUnclassified || f.Status != report.StatusOpen {
		t.Errorf("unit finding = %+v", f)
	}
	if s := skip(rep, "properties"); s == nil || s.Reason != report.SkipNotApplicable {
		t.Errorf("properties skip = %+v, want not-applicable (T2)", s)
	}
	if s := skip(rep, "corpus"); s == nil || s.Reason != report.SkipNoCommand {
		t.Errorf("corpus skip = %+v, want no-command", s)
	}
	if !rep.HasOpen() {
		t.Error("HasOpen = false")
	}

	// reliab's own output (logs, results, the report) leaves the tree hash alone.
	h, err := gitx.TreeHash(root, []string{".reliab/**"})
	if err != nil || h != rep.Run.TreeHash {
		t.Errorf("tree hash after run = %s, report says %s (%v)", h, rep.Run.TreeHash, err)
	}
}

func TestVerifyBudgetKillsAndSkips(t *testing.T) {
	root := repo(t, `commands:
  build: sleep 30
  analyzers: echo never
tiers:
  inner: { budget_seconds: 1 }
`)
	rep := verify(t, root, "inner")
	if rep.Run.Complete {
		t.Fatal("complete = true after a budget overrun")
	}
	if rep.Run.ElapsedSec > 6 {
		t.Errorf("elapsed %.1fs: the killed command was waited on", rep.Run.ElapsedSec)
	}
	for _, id := range []string{"build", "analyzers"} {
		if s := skip(rep, id); s == nil || s.Reason != report.SkipBudget {
			t.Errorf("%s skip = %+v, want budget", id, s)
		}
	}
	if rep.HasOpen() {
		t.Error("a budget overrun alone produced findings")
	}
}

func TestVerifyImpacted(t *testing.T) {
	extra := `commands:
  unit: echo "{impacted}" > .reliab/out/impacted.txt
impacted:
  all: ALL
  map:
    - { glob: "src/a/**", args: A }
    - { glob: "src/b/**", args: B }
`
	tests := []struct {
		name, tier string
		edit       []string
		want       string
	}{
		{"no changes", "inner", nil, "ALL"},
		{"one mapped dir", "inner", []string{"src/a/a.txt"}, "A"},
		{"two mapped dirs", "inner", []string{"src/a/a.txt", "src/b/b.txt"}, "A B"},
		{"ignored files do not widen", "inner", []string{"src/a/a.txt", "docs/x.md"}, "A"},
		{"unmapped file means all", "inner", []string{"src/a/a.txt", "other.txt"}, "ALL"},
		{"prepr always all", "prepr", []string{"src/a/a.txt"}, "ALL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := repo(t, extra)
			for _, f := range tt.edit {
				p := filepath.Join(root, f)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("changed"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			verify(t, root, tt.tier)
			got, err := os.ReadFile(filepath.Join(root, ".reliab/out/impacted.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(got)) != tt.want {
				t.Errorf("{impacted} = %q, want %q", strings.TrimSpace(string(got)), tt.want)
			}
		})
	}
}

func TestVerifyCarriesClassificationForward(t *testing.T) {
	root := repo(t, "commands:\n  analyzers: exit 1\n")
	rep := verify(t, root, "inner")

	rep.Findings[0].Kind = report.KindLegacyDefectOrQuirk
	rep.Findings[0].Classification = &report.Classification{Reason: "known quirk", At: "2026-10-17T00:00:00Z"}
	rep.Findings[0].Acknowledged = &report.Acknowledged{By: "human:test", At: "2026-10-17T00:00:00Z"}
	if _, err := report.Write(root, rep); err != nil {
		t.Fatal(err)
	}

	again := verify(t, root, "inner")
	f := again.Findings[0]
	if f.Kind != report.KindLegacyDefectOrQuirk || f.Classification == nil || f.Status != report.StatusAcknowledged {
		t.Fatalf("finding not carried forward: %+v", f)
	}
	if again.HasOpen() {
		t.Error("an acknowledged finding still counts as open")
	}
}

// An environment failure is about the machine at the time; once it is
// fixed, a failure there must be judged afresh, so the kind does not carry.
func TestVerifyDoesNotCarryEnvironmentForward(t *testing.T) {
	root := repo(t, "commands:\n  analyzers: exit 1\n")
	rep := verify(t, root, "inner")

	rep.Findings[0].Kind = report.KindEnvironment
	rep.Findings[0].Classification = &report.Classification{Reason: "too many consoles in use", At: "2026-10-18T00:00:00Z"}
	rep.Findings[0].Acknowledged = &report.Acknowledged{By: "human:test", At: "2026-10-18T00:00:00Z"}
	if _, err := report.Write(root, rep); err != nil {
		t.Fatal(err)
	}

	again := verify(t, root, "inner")
	f := again.Findings[0]
	if f.Kind != report.KindUnclassified || f.Classification != nil || f.Status != report.StatusOpen {
		t.Fatalf("environment finding carried forward: %+v", f)
	}
}

func TestVerifyUnknownTier(t *testing.T) {
	root := repo(t, "commands:\n  build: \"true\"\n")
	if _, _, err := Run(context.Background(), Options{Root: root, Profile: "reliability-profile.yaml", Tier: "weekly"}); err == nil {
		t.Fatal("unknown tier accepted")
	}
}

// On-demand runs: knowledge/decisions/on-demand-verify.md.
func TestVerifyOnlyRunsChosenCommands(t *testing.T) {
	root := repo(t, `commands:
  build: echo built > .reliab/out/build.txt
  analyzers: echo lint error >&2; exit 3
  unit: echo "{scope}|$RELIAB_SCOPE|$RELIAB_TIER" > .reliab/out/unit.txt
`)
	inner := verify(t, root, "inner")

	rep, path, err := Run(context.Background(), Options{
		Root: root, Profile: "reliability-profile.yaml", Tier: "prepr",
		Only: []string{"unit", "analyzers"}, Scope: []string{"src/a", "src/b/b.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != report.LatestPath(root, report.OnDemand) {
		t.Errorf("written to %s, want the on-demand report", path)
	}
	if _, err := report.Read(path); err != nil {
		t.Fatalf("written report does not read back: %v", err)
	}
	if rep.Run.Tier != report.OnDemand || rep.Run.BudgetSec != 900 {
		t.Errorf("run = %+v, want on-demand under the prepr budget", rep.Run)
	}
	if strings.Join(rep.Run.Only, ",") != "unit,analyzers" || strings.Join(rep.Run.Scope, ",") != "src/a,src/b/b.txt" {
		t.Errorf("only = %v, scope = %v", rep.Run.Only, rep.Run.Scope)
	}
	var ran []string
	for _, c := range rep.Run.Commands {
		ran = append(ran, c.ID)
		if c.Log != ".reliab/out/on-demand-"+c.ID+".log" {
			t.Errorf("%s log = %s", c.ID, c.Log)
		}
	}
	if strings.Join(ran, ",") != "unit,analyzers" || len(rep.Run.Skipped) != 0 {
		t.Errorf("ran %v, skipped %+v; want unit then analyzers only", ran, rep.Run.Skipped)
	}
	if len(rep.Findings) != 1 || rep.Findings[0].ID != "F-analyzers" {
		t.Errorf("findings = %+v", rep.Findings)
	}
	got, err := os.ReadFile(filepath.Join(root, ".reliab/out/unit.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "src/a src/b/b.txt|src/a src/b/b.txt|prepr" {
		t.Errorf("{scope}|RELIAB_SCOPE|RELIAB_TIER = %q", strings.TrimSpace(string(got)))
	}

	// The tier's own report is untouched: an on-demand run never stands in
	// for it.
	again, err := report.Read(report.LatestPath(root, "inner"))
	if err != nil || again.Run.StartedAt != inner.Run.StartedAt || again.Run.Tier != "inner" {
		t.Errorf("inner report changed: %+v (%v)", again, err)
	}
}

func TestVerifyOnlyRejections(t *testing.T) {
	root := repo(t, "commands:\n  build: \"true\"\n")
	tests := []struct {
		name        string
		only, scope []string
		want        string
	}{
		{"undefined command", []string{"unit"}, nil, "defines build"},
		{"not applicable", []string{"mutation_full"}, nil, "T4 is not applicable"},
		{"scope without only", nil, []string{"src"}, "--only"},
		{"missing scope", []string{"build"}, []string{"src/nope"}, "src/nope"},
		{"scope outside the repo", []string{"build"}, []string{".."}, "outside the repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Run(context.Background(), Options{
				Root: root, Profile: "reliability-profile.yaml", Tier: "inner", Only: tt.only, Scope: tt.scope,
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
	if _, err := os.Stat(report.LatestPath(root, report.OnDemand)); !os.IsNotExist(err) {
		t.Errorf("a rejected run wrote a report (%v)", err)
	}
}

// A scope selects impacted args from the files under it, in any tier, and
// ignores what changed.
func TestVerifyScopeNarrowsImpacted(t *testing.T) {
	extra := `commands:
  unit: echo "{impacted}" > .reliab/out/impacted.txt
impacted:
  all: ALL
  map:
    - { glob: "src/a/**", args: A }
    - { glob: "src/b/**", args: B }
`
	tests := []struct {
		name, tier string
		scope      []string
		want       string
	}{
		{"one mapped dir", "prepr", []string{"src/a"}, "A"},
		{"one file", "inner", []string{"src/b/b.txt"}, "B"},
		{"parent of both", "nightly", []string{"src"}, "A B"},
		{"unmapped file means all", "inner", []string{"src/a", "snapshots"}, "ALL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := repo(t, extra)
			testrepo.Write(t, root, "src/b/b.txt", "changed") // a change outside the scope
			testrepo.Write(t, root, "src/a/new.txt", "untracked")
			if _, _, err := Run(context.Background(), Options{
				Root: root, Profile: "reliability-profile.yaml", Tier: tt.tier, Only: []string{"unit"}, Scope: tt.scope,
			}); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(root, ".reliab/out/impacted.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(got)) != tt.want {
				t.Errorf("{impacted} = %q, want %q", strings.TrimSpace(string(got)), tt.want)
			}
		})
	}
}

// Adapters ship beside reliab and resolve by name from profile commands.
func TestVerifyFindsAdaptersBesideReliab(t *testing.T) {
	bin := t.TempDir()
	adapter := "#!/bin/sh\nprintf '{\"schemaVersion\":\"0.1.0\",\"findings\":[{\"test\":\"m1\",\"location\":\"a.go:3:1\"}]}' > \"$1\"\n"
	if err := os.WriteFile(filepath.Join(bin, "reliab-adapter-fake"), []byte(adapter), 0o755); err != nil {
		t.Fatal(err)
	}
	root := repo(t, `commands:
  build: echo ok
  unit:
    run: exit 2; reliab-adapter-fake .reliab/out/f.json
    format: findings
    output: .reliab/out/f.json
  analyzers:
    run: echo tool exit ignored; (exit 2); reliab-adapter-fake .reliab/out/a.json
    format: findings
    output: .reliab/out/a.json
`)
	rep, _, err := Run(context.Background(), Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner", BinDir: bin})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	// unit exited before the adapter ran: no file, so the command failed.
	if f := rep.Findings[1]; f.Command != "unit" || f.ID != "F-unit" {
		t.Errorf("unit finding = %+v", f)
	}
	if f := rep.Findings[0]; f.Command != "analyzers" || f.Test != "m1" || f.Location != "a.go:3:1" {
		t.Errorf("analyzers finding = %+v", f)
	}
}

func TestPrependPath(t *testing.T) {
	sep := string(os.PathListSeparator)
	got := prependPath([]string{"A=1", "PATH=/usr/bin", "B=2"}, "/opt/reliab")
	if strings.Join(got, ",") != "A=1,PATH=/opt/reliab"+sep+"/usr/bin,B=2" {
		t.Errorf("got %v", got)
	}
	if got := prependPath([]string{"A=1"}, "/opt/reliab"); strings.Join(got, ",") != "A=1,PATH=/opt/reliab" {
		t.Errorf("no PATH: got %v", got)
	}
	if got := prependPath([]string{"PATH=/usr/bin"}, ""); got[0] != "PATH=/usr/bin" {
		t.Errorf("empty dir: got %v", got)
	}
}

// The report records the commit it ran on and a mutation command's counts,
// so the PR summary can say tested, killed, and survived and which runs
// belong to the branch (knowledge/decisions/pr-evidence-summary.md).
func TestVerifyRecordsHeadAndMutantCounts(t *testing.T) {
	root := repo(t, `commands:
  build: "true"
  mutation_changed:
    run: cp test/m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
tiers:
  inner: { run: [build, mutation_changed] }
`)
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.3.0","mutants":{"killed":9,"survived":1,"notCovered":2},"findings":[{"test":"src/a/a.go: CONDITIONALS_NEGATION"}]}`)
	rep := verify(t, root, "inner")
	if rep.Run.Head == "" || rep.Run.Head != gitx.Head(root) {
		t.Errorf("head = %q, want %q", rep.Run.Head, gitx.Head(root))
	}
	var counts *report.MutantCounts
	for _, c := range rep.Run.Commands {
		if c.ID == "build" && c.Mutants != nil {
			t.Errorf("build has mutant counts: %+v", c.Mutants)
		}
		if c.ID == "mutation_changed" {
			counts = c.Mutants
		}
	}
	if counts == nil || *counts != (report.MutantCounts{Killed: 9, Survived: 1, NotCovered: 2}) {
		t.Errorf("mutation counts = %+v", counts)
	}
}

// knowledge/decisions/schema-v0-shape.md: a role the codebase needs but has
// no tool for yet is skipped as role-missing, not as not applicable, and
// --only refuses its commands saying so.
func TestRoleMissing(t *testing.T) {
	root := repo(t, "commands:\n  build: \"true\"\n")
	p := filepath.Join(root, "reliability-profile.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	prof := strings.Replace(string(b), "T2: { applicable: false, reason: no property library }", "T2: { missing: true, reason: no property library yet; adding one is the team's call }", 1)
	if prof == string(b) {
		t.Fatal("the test profile has no T2 line to replace")
	}
	if err := os.WriteFile(p, []byte(prof), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := verify(t, root, "inner")
	if s := skip(rep, "properties"); s == nil || s.Reason != report.SkipRoleMissing || s.Detail != "T2: no property library yet; adding one is the team's call" {
		t.Errorf("properties skip = %+v, want role-missing (T2)", s)
	}
	_, _, err = Run(context.Background(), Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner", Only: []string{"properties"}})
	if err == nil || !strings.Contains(err.Error(), "needs T2 but has no tool for it yet") {
		t.Errorf("--only properties: %v", err)
	}
}

// knowledge/decisions/schema-v0-shape.md: integration_tests is a command id
// of its own, run in prepr and nightly but not in the edit loop.
func TestIntegrationTestsRunFromPrepr(t *testing.T) {
	root := repo(t, "commands:\n  integration_tests: echo ran > it.txt\n")
	for tier, want := range map[string]bool{"inner": false, "prepr": true, "nightly": true} {
		_ = os.Remove(filepath.Join(root, "it.txt"))
		verify(t, root, tier)
		if _, err := os.Stat(filepath.Join(root, "it.txt")); (err == nil) != want {
			t.Errorf("%s: ran %v, want %v", tier, err == nil, want)
		}
	}
}
