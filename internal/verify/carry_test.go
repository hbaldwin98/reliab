package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/triage"
)

// knowledge/decisions/finding-classification.md: a command-level finding
// (the command failed as a whole, F-<command>) has one id however it fails,
// so its classification and acknowledgement carry only while the failure
// is the same. A tool reporting several problems by exit code once let a
// person's acknowledgement of one cover every later one.

const carryProfile = "commands:\n  analyzers: cat src/out.txt; exit 1\ntiers:\n  inner: { run: [analyzers] }\n"

var carryAt = time.Date(2036, 10, 19, 9, 0, 0, 0, time.UTC)

// lintRun writes what the failing command prints, runs inner, and returns
// its one finding.
func lintRun(t *testing.T, root, out string) report.Finding {
	t.Helper()
	testrepo.Write(t, root, "src/out.txt", out)
	rep := run(t, Options{Root: root, Tier: "inner"})
	if len(rep.Findings) != 1 || rep.Findings[0].ID != "F-analyzers" || rep.Findings[0].Fingerprint == "" {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	return rep.Findings[0]
}

func classify(t *testing.T, root string) {
	t.Helper()
	if _, err := triage.Classify(root, "F-analyzers", report.KindLegacyDefectOrQuirk, "a known warning", carryAt); err != nil {
		t.Fatal(err)
	}
}

func TestCommandFindingCarriesTheSameFailure(t *testing.T) {
	root := repo(t, carryProfile)
	lintRun(t, root, "a.go:3: unused variable x\n")
	classify(t, root)
	if f := lintRun(t, root, "a.go:3: unused variable x\n"); f.Kind != report.KindLegacyDefectOrQuirk || f.Classification == nil || f.Reopened != "" {
		t.Errorf("same failure: %+v", f)
	}
	if _, err := triage.Acknowledge(root, "F-analyzers", "human:owner", carryAt); err != nil {
		t.Fatal(err)
	}
	if f := lintRun(t, root, "a.go:3: unused variable x\n"); f.Status != report.StatusAcknowledged || f.Acknowledged == nil {
		t.Errorf("same failure, acknowledged: %+v", f)
	}
}

func TestCommandFindingReopensADifferentFailure(t *testing.T) {
	root := repo(t, carryProfile)
	lintRun(t, root, "a.go:3: unused variable x\n")
	classify(t, root)
	f := lintRun(t, root, "a.go:3: unused variable x\nb.go:9: result of append is never used\n")
	if f.Kind != report.KindUnclassified || f.Classification != nil || f.Status != report.StatusOpen ||
		f.Reopened != "the failure changed since it was classified legacy-defect-or-quirk; judge it afresh" {
		t.Errorf("classified, then a new problem: %+v", f)
	}

	lintRun(t, root, "a.go:3: unused variable x\n")
	classify(t, root)
	if _, err := triage.Acknowledge(root, "F-analyzers", "human:owner", carryAt); err != nil {
		t.Fatal(err)
	}
	f = lintRun(t, root, "b.go:9: result of append is never used\n")
	if f.Status != report.StatusOpen || f.Acknowledged != nil || !strings.Contains(f.Reopened, "changed since it was acknowledged by human:owner") {
		t.Errorf("acknowledged, then a different problem: %+v", f)
	}
	var out strings.Builder
	if _, err := triage.Route(&out, root); err != nil || !strings.Contains(out.String(), "  reopened: the failure changed since it was acknowledged by human:owner") {
		t.Errorf("route: %v\n%s", err, out.String())
	}
}

// Durations, timestamps, temp and absolute paths, addresses, and line
// endings differ between runs of one failure; they do not reopen it.
func TestCommandFindingCarriesThroughNoise(t *testing.T) {
	root := repo(t, carryProfile)
	tmp := filepath.ToSlash(os.TempDir())
	noisy := func(dur, at, build, addr, eol string) string {
		return strings.Join([]string{
			"FAIL after " + dur + " at " + at,
			"compile " + tmp + "/go-build" + build + "/b001/x.a",
			"in " + filepath.ToSlash(root) + "/src/a.go:3: unused variable x",
			"goroutine at " + addr,
			"",
		}, eol)
	}
	lintRun(t, root, noisy("1.2s", "2036-10-19T09:00:00Z", "111", "0xc000123456", "\n"))
	classify(t, root)
	f := lintRun(t, root, noisy("3m4.5s", "2036-10-19T11:30:07.25+02:00", "999", "0xc000fedcba", "\r\n"))
	if f.Kind != report.KindLegacyDefectOrQuirk || f.Reopened != "" {
		t.Errorf("noise only: %+v", f)
	}
}

// A report from before fingerprints cannot show the failure is the same,
// so nothing carries from it, and the finding says so.
func TestCommandFindingDoesNotCarryFromAReportWithoutFingerprint(t *testing.T) {
	root := repo(t, carryProfile)
	lintRun(t, root, "a.go:3: unused variable x\n")
	classify(t, root)
	path := report.LatestPath(root, "inner")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, f := range doc["findings"].([]any) {
		delete(f.(map[string]any), "fingerprint")
	}
	raw, _ = json.Marshal(doc)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	f := lintRun(t, root, "a.go:3: unused variable x\n")
	if f.Kind != report.KindUnclassified || f.Classification != nil || !strings.Contains(f.Reopened, "recorded no fingerprint") {
		t.Errorf("old report: %+v", f)
	}
}

// A finding with its own test keeps its classification whatever its
// message says, as before.
func TestTestFindingCarriesWhateverItsMessage(t *testing.T) {
	root := repo(t, "commands:\n  unit:\n    run: printf '{\"schemaVersion\":\"0.4.0\",\"findings\":[{\"test\":\"pkg.TestA\",\"message\":\"%s\"}]}' \"$(cat src/out.txt)\" > out.json\n    format: findings\n    output: out.json\ntiers:\n  inner: { run: [unit] }\n")
	testrepo.Write(t, root, "src/out.txt", "want 1")
	rep := run(t, Options{Root: root, Tier: "inner"})
	id := rep.Findings[0].ID
	if _, err := triage.Classify(root, id, report.KindLegacyDefectOrQuirk, "known", carryAt); err != nil {
		t.Fatal(err)
	}
	testrepo.Write(t, root, "src/out.txt", "want 2")
	rep = run(t, Options{Root: root, Tier: "inner"})
	if f := rep.Findings[0]; f.Kind != report.KindLegacyDefectOrQuirk || f.Fingerprint != "" || f.Reopened != "" {
		t.Errorf("test finding: %+v", f)
	}
}
