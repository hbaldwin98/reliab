package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

func command(rep *report.Report, id string) *report.CommandResult {
	for i := range rep.Run.Commands {
		if rep.Run.Commands[i].ID == id {
			return &rep.Run.Commands[i]
		}
	}
	return nil
}

// A tool that writes one result file per project overwrote a single file
// and lost all but the last project's failures; a glob reads every file.
// Matches from an earlier run are removed before the command runs.
func TestVerifyOutputGlobMergesAndClearsStale(t *testing.T) {
	root := repo(t, `commands:
  unit:
    run: >-
      mkdir -p .reliab/out/unit &&
      printf '<testsuite><testcase classname="A" name="t"><failure>a</failure></testcase></testsuite>' > .reliab/out/unit/a.xml &&
      printf '<testsuite><testcase classname="B" name="t"><failure>b</failure></testcase></testsuite>' > .reliab/out/unit/b.xml;
      exit 1
    format: junit
    output: .reliab/out/unit/*.xml
`)
	testrepo.Write(t, root, ".reliab/out/unit/stale.xml", `<testsuite><testcase name="old"><failure>x</failure></testcase></testsuite>`)
	rep := verify(t, root, "inner")
	var tests []string
	for _, f := range rep.Findings {
		tests = append(tests, f.Test)
	}
	if strings.Join(tests, ",") != "A::t,B::t" {
		t.Fatalf("findings = %v, want both projects and not the stale file", tests)
	}
	if _, err := os.Stat(filepath.Join(root, ".reliab/out/unit/stale.xml")); !os.IsNotExist(err) {
		t.Errorf("stale output not removed: %v", err)
	}
}

// A declared output must be written whatever its format, so a coverage
// command that wrote nothing is not a silent pass.
func TestVerifyMissingOutputOfUnparsedFormat(t *testing.T) {
	root := repo(t, `tiers: { inner: { run: [coverage] } }
commands:
  coverage:
    run: echo wrote nothing
    format: sarif
    output: .reliab/out/cov.sarif
`)
	rep := verify(t, root, "inner")
	if len(rep.Findings) != 1 || !strings.Contains(rep.Findings[0].Message, "not produced") {
		t.Fatalf("findings = %+v, want the missing output reported", rep.Findings)
	}
}

// After a failed build every later command would test stale binaries: they
// are skipped as build-failed, and the build failure is a finding.
func TestVerifyBuildFailureSkipsTheRest(t *testing.T) {
	root := repo(t, `commands:
  build: echo cannot compile >&2; exit 1
  analyzers: echo lint
  unit: echo tests
`)
	rep := verify(t, root, "inner")
	if len(rep.Run.Commands) != 1 || len(rep.Findings) != 1 || rep.Findings[0].ID != "F-build" {
		t.Fatalf("commands = %+v findings = %+v", rep.Run.Commands, rep.Findings)
	}
	for _, id := range []string{"analyzers", "unit"} {
		if s := skip(rep, id); s == nil || s.Reason != report.SkipBuildFailed {
			t.Errorf("%s skip = %+v, want build-failed", id, s)
		}
	}
	if !rep.HasOpen() {
		t.Error("a failed build must not read as a pass")
	}
}

// {base}, RELIAB_BASE, and RELIAB_COMMAND_START reach commands; a
// changed_only coverage report counts only lines changed since the base,
// and says so.
func TestVerifyBaseAndChangedOnly(t *testing.T) {
	root := repo(t, `base: main
tiers: { inner: { run: [unit, coverage] } }
commands:
  unit: test "{base}" = main && test "$RELIAB_BASE" = main && test -n "$RELIAB_COMMAND_START"
  coverage:
    run: >-
      printf '<coverage><packages><package><classes><class filename="src/a/a.txt"><lines><line number="1" hits="1"/><line number="2" hits="0"/><line number="3" hits="0"/></lines></class></classes></package></packages></coverage>' > .reliab/out/cov.xml
    format: cobertura
    output: .reliab/out/cov.xml
    changed_only: true
`)
	testrepo.Write(t, root, "src/a/a.txt", "a\n")
	testrepo.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qam", "newline")
	testrepo.Git(t, root, "checkout", "-q", "-b", "topic")
	testrepo.Write(t, root, "src/a/a.txt", "a\nadded\n")
	rep := verify(t, root, "inner")
	if len(rep.Findings) != 0 {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	if rep.Run.Base != "main" {
		t.Errorf("run base = %q", rep.Run.Base)
	}
	c := command(rep, "coverage")
	if c == nil || c.Coverage == nil || *c.Coverage != (report.LineCounts{Valid: 1, Covered: 0, ChangedSince: "main"}) {
		t.Errorf("coverage = %+v (%+v), want line 2 only (line 1 is unchanged, line 3 is not in the file)", c, c.Coverage)
	}
}

// RELIAB_COMMAND_START is the command's own start, not the run's.
func TestVerifyCommandStart(t *testing.T) {
	root := repo(t, `commands:
  build: printf '%s' "$RELIAB_COMMAND_START" > .reliab/out/start.txt
`)
	before := time.Now().Add(-time.Second)
	verify(t, root, "inner")
	raw, err := os.ReadFile(filepath.Join(root, ".reliab/out/start.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := time.Parse(time.RFC3339Nano, string(raw))
	if err != nil || got.Before(before) {
		t.Errorf("RELIAB_COMMAND_START = %q (%v)", raw, err)
	}
}

// Several matched map entries are joined with the profile's separator.
func TestImpactedSeparator(t *testing.T) {
	root := repo(t, `commands:
  unit: echo "{impacted}" > .reliab/out/impacted.txt
impacted:
  all: ALL
  separator: " | "
  map:
    - { glob: "src/a/**", args: "A" }
    - { glob: "src/b/**", args: "B" }
`)
	testrepo.Write(t, root, "src/a/a.txt", "a2")
	testrepo.Write(t, root, "src/b/b.txt", "b2")
	verify(t, root, "inner")
	raw, _ := os.ReadFile(filepath.Join(root, ".reliab/out/impacted.txt"))
	if got := strings.TrimSpace(string(raw)); got != "A | B" {
		t.Errorf("impacted = %q, want A | B", got)
	}
}

// A previous result that cannot be removed is a finding, and the command
// does not run: its results could not be told from the old ones.
func TestVerifyStaleOutputNotRemovable(t *testing.T) {
	root := repo(t, `commands:
  unit:
    run: echo ran > .reliab/out/ran.txt
    format: junit
    output: .reliab/out/u.xml
`)
	// A directory that is not empty cannot be removed on any OS.
	testrepo.Write(t, root, ".reliab/out/u.xml/inner.txt", "x")
	rep := verify(t, root, "inner")
	c := command(rep, "unit")
	if c == nil || c.ExitCode != -1 || len(rep.Findings) != 1 || !strings.Contains(rep.Findings[0].Message, "cannot remove stale output: ") {
		t.Fatalf("command = %+v findings = %+v", c, rep.Findings)
	}
	if _, err := os.Stat(filepath.Join(root, ".reliab/out/ran.txt")); !os.IsNotExist(err) {
		t.Errorf("the command ran anyway: %v", err)
	}
}

func TestVerifyBaseUnresolved(t *testing.T) {
	root := repo(t, `base: nosuchref
commands:
  unit: echo {base}
`)
	if _, _, err := Run(context.Background(), Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner"}); err == nil || !strings.Contains(err.Error(), "nosuchref") {
		t.Errorf("err = %v, want the unresolved base named", err)
	}
}
