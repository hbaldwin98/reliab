package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/adapters"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/triage"
	"github.com/hbaldwin98/reliab/internal/verify"
	"github.com/hbaldwin98/reliab/pkg/findings"
	"github.com/hbaldwin98/reliab/pkg/snapdiff"
)

// testdata holds the contracts of a checkout service with three providers
// it owns: accepted/ as committed, pending/ as its tests just wrote them.
// The orders contract gained an interaction (a cancel), the billing one
// expects a charge to come back authorized rather than captured, and the
// inventory one is new. Pact's metadata and V4 interaction keys differ in
// every pending file, and the orders file is written compactly with its
// keys in another order.
const (
	pending  = "testdata/pending"
	accepted = "testdata/accepted"
	billing  = "pact checkout -> billing"
	stock    = "pact checkout -> inventory"
	orders   = "pact checkout -> orders"
)

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

func adapt(t *testing.T, exit int, junit, pending, accepted string, scoped bool) []findings.Finding {
	t.Helper()
	out := filepath.Join(t.TempDir(), "contracts.json")
	if err := run(exit, junit, pending, accepted, out, scoped); err != nil {
		t.Fatal(err)
	}
	return read(t, out)
}

// knowledge/decisions/consumer-contracts.md: one labelled finding per
// changed contract, named by consumer and provider, located at the
// accepted file a person approves.
func TestLabels(t *testing.T) {
	got := adapt(t, 0, "", pending, accepted, false)
	if len(got) != 3 {
		t.Fatalf("findings = %+v, want one per changed contract", got)
	}
	want := []struct{ test, loc, diff, msg string }{
		{billing, accepted + "/checkout-billing.json", snapdiff.ValuesChanged, `"state": "authorized"`},
		{stock, accepted + "/checkout-inventory.json", snapdiff.AdditiveOnly, "new contract, none accepted yet"},
		{orders, accepted + "/checkout-orders.json", snapdiff.AdditiveOnly, `+      "description": "a request to cancel order 42",`},
	}
	for i, w := range want {
		f := got[i]
		if f.Test != w.test || f.Location != w.loc || f.Diff != w.diff || !strings.Contains(f.Message, w.msg) {
			t.Errorf("finding %d = %+v, want %s at %s, %s, with %q", i, f, w.test, w.loc, w.diff, w.msg)
		}
		if !strings.Contains(f.Message, "written to "+pending+"/") {
			t.Errorf("%s does not say where the pending contract is: %q", f.Test, f.Message)
		}
		for _, noise := range []string{"pactSpecification", "pactJs", "ffi", `"key"`} {
			if strings.Contains(f.Message, noise) {
				t.Errorf("%s diffs %s: %q", f.Test, noise, f.Message)
			}
		}
	}
}

// Metadata, interaction keys, the order of interactions, and formatting are
// not the contract.
func TestRenderingIsNotAChange(t *testing.T) {
	acc, cur := t.TempDir(), t.TempDir()
	testrepo.Write(t, acc, "orders.json", `{
  "metadata": { "pactSpecification": { "version": "3.0.0" } },
  "provider": { "name": "orders" },
  "consumer": { "name": "checkout" },
  "interactions": [
    { "key": "abc", "description": "b", "providerStates": [{ "name": "s" }], "request": { "method": "GET", "path": "/b" }, "response": { "status": 200 } },
    { "description": "a", "providerStates": [{ "name": "t" }], "request": { "method": "GET", "path": "/a2" }, "response": { "status": 200 } },
    { "description": "a", "providerStates": [{ "name": "s" }], "request": { "method": "GET", "path": "/a" }, "response": { "status": 200 } }
  ]
}`)
	// Another file name, a byte-order mark, one line, keys and
	// interactions in another order, another key and metadata.
	testrepo.Write(t, cur, "checkout-orders.json", "\xef\xbb\xbf"+`{"consumer":{"name":"checkout"},"provider":{"name":"orders"},"interactions":[`+
		`{"description":"a","providerStates":[{"name":"s"}],"request":{"path":"/a","method":"GET"},"response":{"status":200}},`+
		`{"description":"b","key":"def","providerStates":[{"name":"s"}],"request":{"method":"GET","path":"/b"},"response":{"status":200}},`+
		`{"description":"a","providerStates":[{"name":"t"}],"request":{"method":"GET","path":"/a2"},"response":{"status":200}}`+
		`],"metadata":{"pactSpecification":{"version":"3.0.0"},"pactJs":{"version":"13.1.0"}}}`)
	if got := adapt(t, 0, "", cur, acc, false); len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// An accepted contract no test wrote is reported after a clean, whole run
// only: a failing test or a scoped run may simply not have written it.
func TestDroppedContract(t *testing.T) {
	only := t.TempDir()
	testrepo.Write(t, only, "checkout-orders.json", mustRead(t, filepath.Join(accepted, "checkout-orders.json")))
	got := adapt(t, 0, "", only, accepted, false)
	if len(got) != 1 || got[0].Test != billing || got[0].Diff != snapdiff.ValuesChanged || !strings.Contains(got[0].Message, "no test wrote this contract") {
		t.Fatalf("clean run: findings = %+v, want billing dropped", got)
	}
	if got := adapt(t, 0, "", only, accepted, true); len(got) != 0 {
		t.Errorf("scoped run: findings = %+v, want none", got)
	}
	failing := filepath.Join(t.TempDir(), "junit.xml")
	testrepo.Write(t, filepath.Dir(failing), "junit.xml", `<testsuite><testcase name="charges"><failure>boom</failure></testcase></testsuite>`)
	if got := adapt(t, 1, failing, only, accepted, false); len(got) != 1 || got[0].Test != "charges" {
		t.Errorf("failing run: findings = %+v, want the failing test alone", got)
	}
	// Without a JUnit report a failing run reports nothing of its own, so
	// the log is all there is.
	if err := run(1, "", only, accepted, filepath.Join(t.TempDir(), "f.json"), false); err == nil || !strings.Contains(err.Error(), "see its log") {
		t.Errorf("failing run, no junit: err = %v, want the log", err)
	}
}

func TestJUnitKeepsFailures(t *testing.T) {
	got := adapt(t, 1, "testdata/junit.xml", pending, accepted, false)
	if len(got) != 4 {
		t.Fatalf("findings = %+v, want 3 contracts and the mock server mismatch", got)
	}
	f := got[3]
	if f.Diff != "" || !strings.Contains(f.Test, "reserves stock") || !strings.Contains(f.Message, "Mock server mismatch") {
		t.Errorf("failing test = %+v", f)
	}
}

func TestRunErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	empty := t.TempDir()
	notPact := t.TempDir()
	testrepo.Write(t, notPact, "package.json", `{"name": "checkout"}`)
	twice := t.TempDir()
	testrepo.Write(t, twice, "a.json", mustRead(t, filepath.Join(accepted, "checkout-orders.json")))
	testrepo.Write(t, twice, "b.json", mustRead(t, filepath.Join(accepted, "checkout-orders.json")))
	passing := filepath.Join(t.TempDir(), "junit.xml")
	testrepo.Write(t, filepath.Dir(passing), "junit.xml", `<testsuite><testcase name="a"/></testsuite>`)
	tests := []struct {
		name              string
		exit              int
		junit             string
		pending, accepted string
		wantErr           string
	}{
		{"clean run, no contracts anywhere", 0, "", missing, missing, ""},
		{"clean run with junit, nothing changed", 0, passing, accepted, accepted, ""},
		// The pact directory points somewhere else: every accepted
		// contract would read as dropped.
		{"clean run wrote nothing", 0, "", empty, accepted, "point the Pact library's pact directory"},
		{"failed with nothing to report", 1, "", empty, accepted, "no failing test or changed contract"},
		{"failed with all tests passing", 1, passing, accepted, accepted, "no failing test or changed contract"},
		{"build failed before junit", 1, filepath.Join(empty, "junit.xml"), empty, accepted, "without a JUnit report"},
		{"a json file that is not a pact", 0, "", notPact, accepted, "not a pact"},
		{"two files for one pair", 0, "", twice, accepted, "both the contract checkout -> orders"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "findings.json")
			err := run(tt.exit, tt.junit, tt.pending, tt.accepted, out, false)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatalf("output written on error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := read(t, out); len(got) != 0 {
				t.Fatalf("findings = %+v, want none", got)
			}
		})
	}
}

// The whole path: the adapter's file read by reliab verify as the contracts
// command, then shown by reliab report route with the contract advice.
func TestRouteShowsContractAdvice(t *testing.T) {
	findingsFile, err := filepath.Abs(filepath.Join(t.TempDir(), "contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := run(0, "", pending, accepted, findingsFile, false); err != nil {
		t.Fatal(err)
	}
	root := testrepo.New(t, `commands:
  contracts:
    run: cp '`+filepath.ToSlash(findingsFile)+`' .reliab/out/contracts.json
    format: findings
    output: .reliab/out/contracts.json
`)
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if n, err := triage.Route(&buf, root); err != nil || n != 3 {
		t.Fatalf("n=%d err=%v out=%q", n, err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"needs more from the provider",
		"copy the pending contract over the accepted one",
		"--kind characterization-diff",
		"diff: " + snapdiff.Describe(snapdiff.AdditiveOnly),
		`"state": "authorized"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("route lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "a pinned output changed") {
		t.Errorf("route gives the snapshot advice for a contract:\n%s", out)
	}
}
