package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// A baseline is recorded from a fresh, complete report, written by a
// person, and only raised unless lowered on purpose; the gate holds the
// next run to it (knowledge/decisions/mutation-threshold.md).
func TestBaselineProposeWriteAndGate(t *testing.T) {
	root := testrepo.New(t, `protected_paths: ["mutation-baseline.json"]
mutation_baseline: mutation-baseline.json
commands:
  build: "true"
  mutation_full:
    run: cp test/full.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
  mutation_changed:
    run: cp test/changed.json .reliab/out/c.json
    format: findings
    output: .reliab/out/c.json
tiers:
  prepr: { run: [build, mutation_changed] }
`)
	testrepo.Write(t, root, "test/full.json", `{"schemaVersion":"0.4.0","mutants":{"killed":3,"survived":1},"mutantFiles":{"src/a/x.go":{"killed":3,"survived":1}},"findings":[{"test":"src/a/x.go: m","location":"src/a/x.go:1:1"}]}`)
	testrepo.Write(t, root, "test/changed.json", `{"schemaVersion":"0.4.0","mutants":{"killed":1,"survived":1},"mutantFiles":{"src/a/x.go":{"killed":1,"survived":1}},"findings":[{"test":"src/a/x.go: m","location":"src/a/x.go:1:1"}]}`)
	t.Chdir(root)

	if code, _, errOut := run("baseline", "propose", "--scope", "src/a/**"); code != ExitConfig || !strings.Contains(errOut, "no readable on-demand report") {
		t.Fatalf("propose with no report: exit %d, %s", code, errOut)
	}
	if code, out, errOut := run("verify", "--only", "mutation_full"); code != ExitFindings {
		t.Fatalf("verify: exit %d\n%s%s", code, out, errOut)
	}
	code, out, errOut := run("baseline", "propose", "--scope", "src/a/**")
	if code != ExitOK || !strings.Contains(out, "# src/a/**: 75.0% (3 killed of 4 counted), new") || !strings.Contains(out, `"score": 75`) {
		t.Fatalf("propose: exit %d\n%s%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(root, "mutation-baseline.json")); !os.IsNotExist(err) {
		t.Fatal("propose wrote the baseline")
	}
	if code, out, errOut := run("baseline", "write", "--scope", "src/a/**"); code != ExitOK || !strings.Contains(out, "wrote mutation-baseline.json") {
		t.Fatalf("write: exit %d\n%s%s", code, out, errOut)
	}
	// Writing it changed the tree: a later proposal needs a fresh run.
	if code, _, errOut := run("baseline", "propose"); code != ExitConfig || !strings.Contains(errOut, "stale") {
		t.Fatalf("propose from a stale report: exit %d, %s", code, errOut)
	}

	// The changed code scores 50% in a scope recorded at 75%.
	if code, _, _ := run("verify", "--tier", "prepr"); code != ExitFindings {
		t.Fatalf("prepr: exit %d", code)
	}
	code, out, _ = run("gate", "mutation")
	if code != ExitFindings || !strings.Contains(out, "FAIL mutation_changed: baseline src/a/** 75%: 50.0% (1 killed of 2 counted), short by 1") {
		t.Fatalf("gate: exit %d\n%s", code, out)
	}

	// Lowering is refused without --allow-lower.
	testrepo.Write(t, root, "test/full.json", `{"schemaVersion":"0.4.0","mutants":{"killed":1,"survived":1},"mutantFiles":{"src/a/x.go":{"killed":1,"survived":1}},"findings":[{"test":"src/a/x.go: m","location":"src/a/x.go:1:1"}]}`)
	run("verify", "--only", "mutation_full")
	if code, _, errOut := run("baseline", "write"); code != ExitConfig || !strings.Contains(errOut, "src/a/** 75.0% -> 50.0%") {
		t.Fatalf("lowering: exit %d, %s", code, errOut)
	}
	if code, out, errOut := run("baseline", "write", "--allow-lower"); code != ExitOK || !strings.Contains(out, "was 75.0%") {
		t.Fatalf("lowering on purpose: exit %d\n%s%s", code, out, errOut)
	}
}

// The gate passes, exit 0, when every check is met
// (knowledge/decisions/mutation-threshold.md).
func TestGateMutationPasses(t *testing.T) {
	root := testrepo.New(t, `commands:
  mutation_changed:
    run: cp test/m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
    threshold: 80
tiers:
  prepr: { run: [mutation_changed] }
`)
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{"killed":9,"survived":1},"findings":[{"test":"src/a/x.go: m","location":"src/a/x.go:1:1"}]}`)
	t.Chdir(root)
	run("verify", "--tier", "prepr")
	code, out, errOut := run("gate", "mutation")
	if code != ExitOK || !strings.Contains(out, "ok   mutation_changed: threshold 80%: 90.0% (9 killed of 10 counted), met") {
		t.Fatalf("gate: exit %d\n%s%s", code, out, errOut)
	}
}

// classify records the change and warns, exit 0, when the mutation scores
// could not be recomputed (mutation-threshold.md).
func TestClassifyWarnsWhenNotRescored(t *testing.T) {
	root := testrepo.New(t, `protected_paths: ["mutation-baseline.json"]
mutation_baseline: mutation-baseline.json
commands:
  mutation_changed:
    run: cp test/m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
tiers:
  prepr: { run: [mutation_changed] }
`)
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{"killed":1,"survived":1},"findings":[{"test":"src/a/x.go: m","location":"src/a/x.go:1:1"}]}`)
	t.Chdir(root)
	run("verify", "--tier", "prepr")
	testrepo.Write(t, root, "mutation-baseline.json", "{")
	r, err := report.Read(report.LatestPath(root, "prepr"))
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run("findings", "classify", r.Findings[0].ID, "--kind", "requirement-silent", "--reason", "is it intended?")
	if code != ExitOK || !strings.Contains(out, "classified requirement-silent") || !strings.Contains(errOut, "reliab: warning: recorded, but the mutation scores were not recomputed") {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
}

// findings ack warns the same way (mutation-threshold.md).
func TestAckWarnsWhenNotRescored(t *testing.T) {
	root := testrepo.New(t, `commands:
  mutation_changed:
    run: cp test/m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
tiers:
  prepr: { run: [mutation_changed] }
`)
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{"killed":1,"survived":1},"findings":[{"test":"src/a/x.go: m","location":"src/a/x.go:1:1"}]}`)
	t.Chdir(root)
	run("verify", "--tier", "prepr")
	r, err := report.Read(report.LatestPath(root, "prepr"))
	if err != nil {
		t.Fatal(err)
	}
	id := r.Findings[0].ID
	if code, _, errOut := run("findings", "classify", id, "--kind", "equivalent-mutant", "--reason", "unreachable"); code != ExitOK || errOut != "" {
		t.Fatalf("classify: exit %d, %s", code, errOut)
	}
	testrepo.Write(t, root, "reliability-profile.yaml", "not: [a profile")
	code, out, errOut := run("findings", "ack", id, "--by", "human:t")
	if code != ExitOK || !strings.Contains(out, "acknowledged by human:t") || !strings.Contains(errOut, "reliab: warning: recorded, but the mutation scores were not recomputed") {
		t.Fatalf("ack: exit %d\n%s%s", code, out, errOut)
	}
}
