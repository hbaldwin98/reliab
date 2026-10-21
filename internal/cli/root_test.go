package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/version"
)

func run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Execute(args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestHelpExitsOK(t *testing.T) {
	code, out, _ := run("--help")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	for _, sub := range []string{"profile", "verify", "report", "findings", "characterize", "gate", "hook", "doctor"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help does not list %q", sub)
		}
	}
}

// The version command prints what .reliab-version pins, as --version does.
func TestVersion(t *testing.T) {
	code, out, _ := run("version")
	if code != ExitOK || out != version.Version+"\n" {
		t.Fatalf("version: exit %d, stdout %q, want %q", code, out, version.Version)
	}
	if _, out, _ := run("--version"); !strings.Contains(out, version.Version) {
		t.Errorf("--version = %q", out)
	}
}

func TestProfileValidate(t *testing.T) {
	code, out, errOut := run("profile", "validate", "-f", "../../reliability-profile.yaml")
	if code != ExitOK || !strings.Contains(out, "ok:") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if code, _, _ := run("profile", "validate", "-f", "testdata/does-not-exist.yaml"); code != ExitConfig {
		t.Fatalf("missing file: exit code = %d, want %d", code, ExitConfig)
	}
}

func TestHookBadInputDoesNotBlock(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"hook", "pre-bash"}, strings.NewReader("not json"), &out, &errOut)
	if code == 2 || code == 0 {
		t.Fatalf("exit code = %d; a hook error must exit 1 (2 would block the tool call)", code)
	}
	if out.Len() != 0 {
		t.Errorf("a failed hook wrote a decision: %q", out.String())
	}
}

// --scope paths are relative to where reliab runs, like any path argument
// (knowledge/decisions/on-demand-verify.md).
func TestVerifyScopeIsRelativeToWorkingDir(t *testing.T) {
	root := testrepo.New(t, "commands:\n  build: \"true\"\n")
	t.Chdir(filepath.Join(root, "src"))
	code, out, errOut := run("verify", "--only", "build", "--scope", "a")
	if code != ExitOK {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	rep, err := report.Read(report.LatestPath(root, report.OnDemand))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rep.Run.Scope, ",") != "src/a" {
		t.Errorf("scope = %v, want src/a", rep.Run.Scope)
	}
	if code, _, _ := run("verify", "--only", "build", "--scope", "src/a"); code != ExitConfig {
		t.Errorf("a path relative to the root, not the working dir: exit %d, want %d", code, ExitConfig)
	}
}

// knowledge/decisions/named-tests.md: --tests is one test run per flag,
// never comma-split; --mutation-tests is comma-separated or repeated. A run
// that waits for tests to be named exits 3 and says which command waits.
func TestVerifyNamedTestsFlags(t *testing.T) {
	root := testrepo.New(t, "commands:\n  build: \"true\"\n  store_tests:\n    run: \"true\"\n    needs: [tests]\n"+
		"tiers:\n  inner: { run: [build, store_tests] }\n")
	t.Chdir(root)
	code, out, errOut := run("verify")
	if code != ExitIncomplete || !strings.Contains(errOut, "incomplete: store_tests: no tests named: re-run with --tests") {
		t.Errorf("no tests named: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	// The run waits for tests, not the budget, and says only that
	// (knowledge/decisions/named-tests.md: "verify exits 3 and names the
	// waiting commands").
	if strings.Contains(errOut, "budget") {
		t.Errorf("no tests named, budget not exhausted: stderr %q", errOut)
	}
	code, out, errOut = run("verify", "--tests", "--where 'a,b'", "--tests", "--test C", "--mutation-tests", "X,Y", "--mutation-tests", "Z")
	if code != ExitOK {
		t.Fatalf("tests named: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	rep, err := report.Read(report.LatestPath(root, "inner"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rep.Run.Tests, "|") != "--where 'a,b'|--test C" || strings.Join(rep.Run.MutationTests, "|") != "X|Y|Z" {
		t.Errorf("tests %q, mutation tests %q", rep.Run.Tests, rep.Run.MutationTests)
	}
}

func TestUnknownCommandIsConfigError(t *testing.T) {
	if code, _, _ := run("nope"); code != ExitConfig {
		t.Fatalf("exit code = %d, want %d", code, ExitConfig)
	}
}

// The command surface in knowledge/decisions/pr-evidence-summary.md and
// reviewer-verdict.md: summary needs a report, prints Markdown or JSON, and
// shows a recorded verdict; verdict refuses a pass without a re-run.
func TestReportSummaryAndVerdict(t *testing.T) {
	root := testrepo.New(t, "commands:\n  build: \"true\"\n")
	t.Chdir(root)
	if code, _, errOut := run("report", "summary"); code != ExitConfig || !strings.Contains(errOut, "no readable prepr report") {
		t.Fatalf("summary without a report: exit %d, stderr %q", code, errOut)
	}
	if code, _, errOut := run("verify", "--tier", "prepr"); code != ExitOK {
		t.Fatalf("verify: exit %d, %s", code, errOut)
	}
	code, out, errOut := run("report", "summary")
	if code != ExitOK || !strings.HasPrefix(out, "### Evidence: reliab prepr report") || !strings.Contains(out, "| build | 0 | 0 |") {
		t.Fatalf("summary: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if code, out, _ := run("report", "summary", "--format", "json"); code != ExitOK || !strings.Contains(out, `"tier": "prepr"`) {
		t.Fatalf("summary json: exit %d, %q", code, out)
	}
	if code, _, errOut := run("report", "verdict", "--outcome", "pass", "--by", "reliab:reviewer", "--notes", "looked"); code != ExitConfig || !strings.Contains(errOut, "--worktree") {
		t.Fatalf("pass without a re-run: exit %d, stderr %q", code, errOut)
	}
	code, out, errOut = run("report", "verdict", "--outcome", "fail", "--by", "reliab:reviewer", "--notes", "a new test asserts nothing")
	if code != ExitOK || !strings.Contains(out, "verdict fail by reliab:reviewer") {
		t.Fatalf("fail: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if _, out, _ := run("report", "summary"); !strings.Contains(out, "Reviewer verdict: **fail** by reliab:reviewer") {
		t.Fatalf("summary does not show the verdict:\n%s", out)
	}
}

// knowledge/decisions/protected-path-approval.md: approve takes paths
// relative to where it runs, and gate protected then lists the approved
// change apart and passes; an edit to the file voids the approval.
func TestApproveAndGateProtected(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\"snapshots/**\"]\ncommands:\n  build: \"true\"\n")
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, "snapshots/seed.txt", "case")
	t.Chdir(filepath.Join(root, "snapshots"))
	if code, out, _ := run("gate", "protected", "--base", "main"); code != ExitFindings || !strings.Contains(out, "snapshots/seed.txt (protected by snapshots/**)") || strings.Contains(out, "approved by a person") {
		t.Fatalf("unapproved: exit %d, %q", code, out)
	}
	if code, _, errOut := run("approve", "seed.txt", "--by", "reliab:reviewer"); code != ExitConfig || !strings.Contains(errOut, "human:<name>") {
		t.Fatalf("an agent identity: exit %d, %q", code, errOut)
	}
	if code, out, errOut := run("approve", "seed.txt", "--by", "human:owner", "--base", "main"); code != ExitOK || !strings.Contains(out, "snapshots/seed.txt approved by human:owner against main") {
		t.Fatalf("approve: exit %d, %q, %q", code, out, errOut)
	}
	code, out, _ := run("gate", "protected", "--base", "main")
	if code != ExitOK || !strings.Contains(out, "approved by a person") || !strings.Contains(out, "  snapshots/seed.txt (protected by snapshots/**) approved by human:owner") {
		t.Fatalf("approved: exit %d, %q", code, out)
	}
	testrepo.Write(t, root, "snapshots/seed.txt", "case, edited")
	if code, _, _ := run("gate", "protected", "--base", "main"); code != ExitFindings {
		t.Fatalf("edited after approval: exit %d, want %d", code, ExitFindings)
	}
	if code, _, errOut := run("approve", "../../elsewhere", "--by", "human:owner", "--base", "main"); code != ExitConfig || !strings.Contains(errOut, "outside the repository") {
		t.Fatalf("outside path: exit %d, %q", code, errOut)
	}
}

// findings ack accepts every human kind (triage.Acknowledge checks
// report.HumanKinds), so its help names each one; a person reading --help
// must not think a below-threshold finding cannot be acknowledged.
// Source: knowledge/decisions/finding-classification.md (per-kind policy
// lives in report.Kinds).
func TestFindingsAckHelpNamesEveryHumanKind(t *testing.T) {
	code, out, errOut := run("findings", "ack", "--help")
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, k := range report.HumanKinds {
		if !strings.Contains(out, k) {
			t.Errorf("findings ack --help does not name human kind %q:\n%s", k, out)
		}
	}
}
