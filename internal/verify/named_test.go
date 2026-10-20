package verify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// Every rule tested here is stated in knowledge/decisions/named-tests.md.

// echoSelection writes what each command received: {tests} and
// {mutation_tests} one argument per line, and the environment as is.
const echoSelection = `commands:
  unit: >-
    printf '%s\n' {tests} > .reliab/out/tests.txt;
    printf '%s\n' {mutation_tests} > .reliab/out/mutation-tests.txt;
    printf '%s' "$RELIAB_TESTS" > .reliab/out/tests.json;
    printf '%s' "$RELIAB_MUTATION_TESTS" > .reliab/out/mutation-tests.json
tiers:
  inner: { run: [unit] }
  prepr: { run: [unit] }
`

func readOut(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, OutDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func jsonOut(t *testing.T, root, name string) []string {
	t.Helper()
	var got []string
	if err := json.Unmarshal([]byte(readOut(t, root, name)), &got); err != nil {
		t.Fatalf("%s is not a JSON list of strings: %v", name, err)
	}
	return got
}

// A spec is opaque text: quotes, spaces, shell syntax, and a placeholder
// inside it reach the command unchanged, one argument per spec.
func TestNamedTestsReachCommandsAsGiven(t *testing.T) {
	tests := []string{
		"--project Data.Tests --where 'cat==QualityGate' --test Ns.When_UpdateGroups",
		`--filter "a b" $HOME; echo {seed}`,
	}
	mutation := []string{"Ns.When_UpdateGroups", "Ns.When_CreateGroups.Should_Create"}
	for _, only := range [][]string{nil, {"unit"}} {
		root := repo(t, echoSelection)
		rep := run(t, Options{Root: root, Tier: "prepr", Only: only, Tests: tests, MutationTests: mutation})
		if got := readOut(t, root, "tests.txt"); got != strings.Join(tests, "\n")+"\n" {
			t.Errorf("only %v: {tests} gave the arguments\n%s", only, got)
		}
		if got := readOut(t, root, "mutation-tests.txt"); got != strings.Join(mutation, "\n")+"\n" {
			t.Errorf("only %v: {mutation_tests} gave the arguments\n%s", only, got)
		}
		if got := jsonOut(t, root, "tests.json"); !slices.Equal(got, tests) {
			t.Errorf("only %v: RELIAB_TESTS = %q", only, got)
		}
		if got := jsonOut(t, root, "mutation-tests.json"); !slices.Equal(got, mutation) {
			t.Errorf("only %v: RELIAB_MUTATION_TESTS = %q", only, got)
		}
		if !slices.Equal(rep.Run.Tests, tests) || !slices.Equal(rep.Run.MutationTests, mutation) {
			t.Errorf("only %v: the report records tests %q, mutation tests %q", only, rep.Run.Tests, rep.Run.MutationTests)
		}
		if !rep.Run.Complete || rep.HasOpen() {
			t.Errorf("only %v: run = %+v", only, rep.Run)
		}
	}
}

// A selection left in the caller's environment never applies to a run that
// named none.
func TestNamedTestsDoNotLeakFromTheEnvironment(t *testing.T) {
	t.Setenv("RELIAB_TESTS", `["leaked"]`)
	t.Setenv("RELIAB_MUTATION_TESTS", `["leaked"]`)
	root := repo(t, echoSelection)
	rep := run(t, Options{Root: root, Tier: "inner"})
	for _, name := range []string{"tests.json", "mutation-tests.json"} {
		if got := readOut(t, root, name); got != "[]" {
			t.Errorf("%s = %q, want []", name, got)
		}
	}
	if got := readOut(t, root, "tests.txt"); strings.TrimSpace(got) != "" {
		t.Errorf("{tests} with nothing named gave %q", got)
	}
	if rep.Run.Tests != nil || rep.Run.MutationTests != nil {
		t.Errorf("the report records a selection nobody named: %q, %q", rep.Run.Tests, rep.Run.MutationTests)
	}
}

func TestNamedTestsRejectAnEmptySpec(t *testing.T) {
	root := repo(t, echoSelection)
	for _, opt := range []Options{{Tests: []string{"  "}}, {MutationTests: []string{"a", ""}}} {
		opt.Root, opt.Tier, opt.Profile = root, "inner", "reliability-profile.yaml"
		if _, _, err := Run(context.Background(), opt); err == nil || !strings.Contains(err.Error(), "names no tests") {
			t.Errorf("%+v: err = %v", opt, err)
		}
	}
}

const needsTests = `commands:
  store_tests:
    run: "true"
    needs: [tests]
    relevant_when: ["src/data/**"]
  mutation_changed:
    run: "true"
    needs: [mutation_tests]
  unit: "true"
tiers:
  inner: { run: [store_tests, mutation_changed, unit] }
  prepr: { run: [store_tests, mutation_changed, unit] }
  nightly: { run: [store_tests, mutation_changed, unit] }
`

// A relevant command that needs tests, run without them, leaves the run
// incomplete with the reason; the commands after it still run.
func TestNeedsMakesTheRunIncompleteWithoutTests(t *testing.T) {
	root := repo(t, needsTests)
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, "src/data/repo.txt", "changed")
	for _, tier := range []string{"inner", "prepr", "nightly"} {
		rep := run(t, Options{Root: root, Tier: tier})
		if rep.Run.Complete {
			t.Errorf("%s: complete with no tests named", tier)
		}
		want := map[string]string{
			"store_tests":      "no tests named: re-run with --tests",
			"mutation_changed": "no mutation tests named: re-run with --mutation-tests",
		}
		for id, detail := range want {
			if s := skip(rep, id); s == nil || s.Reason != report.SkipNeedsInput || s.Detail != detail {
				t.Errorf("%s: %s skip = %+v, want needs-input %q", tier, id, s, detail)
			}
		}
		if ran(rep, "unit") == nil {
			t.Errorf("%s: unit did not run after a command waiting for tests; skipped %+v", tier, rep.Run.Skipped)
		}
		if len(rep.NeedsInput()) != 2 || rep.HasOpen() {
			t.Errorf("%s: a missing input became a finding or was lost: %+v", tier, rep)
		}
	}

	rep := run(t, Options{Root: root, Tier: "prepr", Tests: []string{"--test A"}})
	if ran(rep, "store_tests") == nil || skip(rep, "mutation_changed") == nil || rep.Run.Complete {
		t.Errorf("tests named, mutation tests not: run = %+v", rep.Run)
	}
	rep = run(t, Options{Root: root, Tier: "prepr", Tests: []string{"--test A"}, MutationTests: []string{"A"}})
	if !rep.Run.Complete || len(rep.Run.Skipped) != 0 || len(rep.Run.Commands) != 3 {
		t.Errorf("everything named: run = %+v", rep.Run)
	}
}

// On a change the command cannot affect it is skipped as not relevant, as
// without needs, and the run stays complete.
func TestNeedsOnAnIrrelevantChangeIsSkippedAsToday(t *testing.T) {
	root := repo(t, needsTests)
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, "src/a/a.txt", "changed")
	rep := run(t, Options{Root: root, Tier: "prepr", MutationTests: []string{"A"}})
	if s := skip(rep, "store_tests"); s == nil || s.Reason != report.SkipNotRelevant {
		t.Errorf("store_tests skip = %+v, want not-relevant", s)
	}
	if !rep.Run.Complete {
		t.Errorf("run incomplete: %+v", rep.Run.Skipped)
	}
}

func TestShellWords(t *testing.T) {
	cases := map[string][]string{
		"":                 nil,
		"'a b'":            {"a b"},
		`'it'\''s' '$x'`:   {"it's", "$x"},
		`'--where "c==Q"'`: {`--where "c==Q"`},
	}
	for want, items := range cases {
		if got := shellWords(items); got != want {
			t.Errorf("shellWords(%q) = %s, want %s", items, got, want)
		}
	}
}
