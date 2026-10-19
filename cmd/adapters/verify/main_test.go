package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/adapters"
	"github.com/hbaldwin98/reliab/pkg/findings"
)

// A TRX report in the shape a .NET test run writes: a snapshot test whose
// output changed, and an ordinary failing test.
const trx = `<?xml version="1.0" encoding="utf-8"?>
<TestRun xmlns="http://microsoft.com/schemas/VisualStudio/TeamTest/2010">
  <TestDefinitions>
    <UnitTest name="BulkQuote" id="s"><TestMethod className="Shop.Tests.QuoteSnapshots" name="BulkQuote"/></UnitTest>
    <UnitTest name="Rejects" id="u"><TestMethod className="Shop.Tests.Orders" name="Rejects"/></UnitTest>
  </TestDefinitions>
  <Results>
    <UnitTestResult testId="s" testName="BulkQuote" outcome="Failed"><Output><ErrorInfo><Message>VerifyException : NotEqual</Message></ErrorInfo></Output></UnitTestResult>
    <UnitTestResult testId="u" testName="Rejects" outcome="Failed"><Output><ErrorInfo><Message>Expected 1</Message></ErrorInfo></Output></UnitTestResult>
  </Results>
</TestRun>`

func put(t *testing.T, dir, name, body string, mod time.Time) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, out string) []findings.Finding {
	t.Helper()
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapters.ParseFindings(raw)
	if err != nil {
		t.Fatalf("reliab cannot read %s: %v", raw, err)
	}
	return got
}

func TestRun(t *testing.T) {
	root := t.TempDir()
	start := time.Now().Add(-time.Minute)
	old := start.Add(-time.Hour)
	put(t, root, "out/snap.trx", trx, start)
	// An earlier run's report, named by time, still beside this one's.
	put(t, root, "out/old.trx", strings.ReplaceAll(trx, "Rejects", "Gone"), old)
	// The accepted snapshot has a byte-order mark, as Verify writes it.
	put(t, root, "tests/Snapshots/QuoteSnapshots.BulkQuote.verified.txt", "\xEF\xBB\xBF{\n  Sku: A-1,\n  Total: 53.89\n}", old)
	put(t, root, "tests/Snapshots/QuoteSnapshots.BulkQuote.received.txt", "{\n  Sku: A-1,\n  Total: 53.88\n}", start.Add(time.Second))
	// A received file nothing claims: a new snapshot.
	put(t, root, "tests/Snapshots/Other.New.received.txt", "{\n  A: 1\n}", start.Add(time.Second))
	// Left by an earlier run (a mutation tool runs the snapshot tests).
	put(t, root, "tests/Snapshots/Old.Stale.received.txt", "{}", old)
	put(t, root, "tests/Snapshots/Old.Stale.verified.txt", "{ x }", old)
	// Build output is never searched.
	put(t, root, "tests/bin/Debug/X.Y.received.txt", "{}", start.Add(time.Second))

	out := filepath.Join(root, "findings.json")
	if err := run(1, filepath.Join(root, "out", "*.trx"), "", root, start, out); err != nil {
		t.Fatal(err)
	}
	got := read(t, out)
	if len(got) != 3 {
		t.Fatalf("findings = %+v, want the snapshot test, the other failure, and the new snapshot", got)
	}
	snap := got[0]
	if snap.Test != "Shop.Tests.QuoteSnapshots.BulkQuote" || snap.Location != "tests/Snapshots/QuoteSnapshots.BulkQuote.verified.txt" || snap.Diff != "values-changed" {
		t.Errorf("snapshot finding = %+v", snap)
	}
	if !strings.Contains(snap.Message, "- ") || !strings.Contains(snap.Message, "VerifyException") {
		t.Errorf("snapshot message = %q, want the labelled diff and the test's message", snap.Message)
	}
	if got[1].Test != "Shop.Tests.Orders.Rejects" || got[1].Diff != "" {
		t.Errorf("ordinary failure = %+v", got[1])
	}
	if got[2].Test != "tests/Snapshots/Other.New.verified.txt" || got[2].Diff != "additive-only" || !strings.Contains(got[2].Message, "new snapshot") {
		t.Errorf("unclaimed snapshot = %+v", got[2])
	}
}

func TestRunErrors(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "findings.json")
	if err := run(1, filepath.Join(root, "none.trx"), "", root, time.Time{}, out); err == nil {
		t.Error("want error without a test report")
	}
	put(t, root, "stale.trx", trx, time.Now().Add(-time.Hour))
	if err := run(0, filepath.Join(root, "stale.trx"), "", root, time.Now().Add(-time.Minute), out); err == nil || !strings.Contains(err.Error(), "written by this run") {
		t.Errorf("only a stale report: %v", err)
	}
	put(t, root, "pass.trx", `<TestRun><Results/></TestRun>`, time.Now())
	if err := run(1, filepath.Join(root, "pass.trx"), "", root, time.Time{}, out); err == nil || !strings.Contains(err.Error(), "exited 1") {
		t.Errorf("failing run with nothing to report: %v", err)
	}
	if err := run(0, filepath.Join(root, "pass.trx"), "", root, time.Time{}, out); err != nil {
		t.Fatal(err)
	}
	if got := read(t, out); len(got) != 0 {
		t.Errorf("passing run = %+v", got)
	}
}

func TestLastSegment(t *testing.T) {
	for in, want := range map[string]string{
		"Ns.Class.Method":       "Method",
		"Ns.Class.Param(1,2)":   "Param",
		"mod::tests::case_name": "case_name",
		"":                      "",
	} {
		if got := lastSegment(in); got != want {
			t.Errorf("lastSegment(%q) = %q, want %q", in, got, want)
		}
	}
}
