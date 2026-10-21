package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// altProfile is a second profile beside the default one: it leaves notes/
// out of the tree hash, holds a mutation command to a threshold, and
// protects nothing. The default profile protects snapshots/ and has no
// mutation command.
const altProfile = "schemaVersion: 0.1.0\nname: alt\nstarting_condition: greenfield\n" +
	"paths: { product: [\"src/**\"], tests: [\"test/**\"], ignore: [\"docs/**\"] }\n" + testrepo.Roles +
	`freshness_exclude: ["notes/**"]
commands:
  mutation_changed:
    run: cp test/m.json .reliab/out/m.json
    format: findings
    output: .reliab/out/m.json
    threshold: 80
tiers:
  prepr: { run: [mutation_changed] }
`

// verify -f records the profile it ran with (report 0.12.0), and the
// commands that judge that report read it: its freshness excludes, tiers,
// and thresholds are the ones the run used. Protection still reads the
// default profile (knowledge/decisions/protected-path-approval.md: the
// profile guards its own protected paths, so an unprotected second profile
// must not drop them).
func TestReportCommandsReadTheProfileTheRunUsed(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\"snapshots/**\"]\ncommands:\n  build: \"true\"\n")
	testrepo.Git(t, root, "checkout", "-q", "-b", "feature")
	testrepo.Write(t, root, "alt.yaml", altProfile)
	testrepo.Write(t, root, "snapshots/s.txt", "changed")
	// 3 killed, 1 survived: 75%, below the threshold of 80.
	testrepo.Write(t, root, "test/m.json", `{"schemaVersion":"0.4.0","mutants":{"killed":3,"survived":1},"findings":[{"test":"src/a/a.go: CONDITIONALS_NEGATION","location":"src/a/a.go:3:1"}]}`)
	t.Chdir(root)
	if code, _, errOut := run("verify", "-f", "alt.yaml", "--tier", "prepr"); code != ExitFindings {
		t.Fatalf("verify: exit %d, %s", code, errOut)
	}
	rep, err := report.Read(report.LatestPath(root, "prepr"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Run.ProfilePath != "alt.yaml" {
		t.Fatalf("profilePath = %q, want alt.yaml", rep.Run.ProfilePath)
	}
	// Outside the tree hash under alt.yaml, inside it under the default.
	testrepo.Write(t, root, "notes/n.txt", "a note")

	code, out, errOut := run("report", "summary", "--format", "json", "--base", "main")
	if code != ExitOK {
		t.Fatalf("summary: exit %d, %s", code, errOut)
	}
	var s struct {
		Fresh     bool     `json:"fresh"`
		NotInTier []string `json:"notInTier"`
		Protected []struct {
			Path string `json:"path"`
		} `json:"protected"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !s.Fresh || len(s.NotInTier) != 0 {
		t.Errorf("summary judged by another profile: fresh %v, not in tier %v", s.Fresh, s.NotInTier)
	}
	if len(s.Protected) != 1 || s.Protected[0].Path != "snapshots/s.txt" {
		t.Errorf("protected = %+v, want snapshots/s.txt from the default profile", s.Protected)
	}

	code, out, errOut = run("gate", "mutation")
	if code != ExitFindings || !strings.Contains(out, "FAIL mutation_changed:") || strings.Contains(out, "stale") {
		t.Errorf("gate mutation: exit %d, stdout %q, stderr %q; want mutation_changed below its threshold, fresh", code, out, errOut)
	}

	// Classifying the survivor re-scores the report by alt.yaml's
	// threshold, so the shortfall stays.
	var survivor string
	for _, f := range rep.Findings {
		if f.Kind != report.KindBelowThreshold {
			survivor = f.ID
		}
	}
	if code, _, errOut := run("findings", "classify", survivor, "--kind", report.KindRequirementSilent, "--reason", "should a negated retry stop?"); code != ExitOK {
		t.Fatalf("classify: exit %d, %s", code, errOut)
	}
	after, err := report.Read(report.LatestPath(root, "prepr"))
	if err != nil {
		t.Fatal(err)
	}
	short := false
	for _, f := range after.Findings {
		short = short || f.Kind == report.KindBelowThreshold
	}
	if !short {
		t.Errorf("re-scored by another profile: findings %+v", after.Findings)
	}

	if code, _, errOut := run("report", "verdict", "--outcome", "fail", "--by", "reliab:reviewer", "--notes", "n"); code != ExitOK {
		t.Errorf("verdict on a fresh report: exit %d, %s", code, errOut)
	}
}
