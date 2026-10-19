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

// testdata holds two insta snapshot pairs in the shape of a Rust
// service's protocol transcripts: an accepted .snap under
// workspace/ and the .snap.new insta wrote for it under pending/, as
// INSTA_PENDING_DIR lays them out. In connecting_and_greeting a capability
// was added after the last one; in a_job_from_start_to_end a job's final
// state changed.
const (
	snaps    = "snapshots/"
	greeting = snaps + "service__test_host__tests__session__connecting_and_greeting.snap"
	job      = snaps + "service__test_host__tests__jobs__a_job_from_start_to_end.snap"
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

func TestLabels(t *testing.T) {
	out := filepath.Join(t.TempDir(), "snapshots.json")
	// insta's test run fails when a snapshot does not match.
	if err := run(101, "", "testdata/workspace", "testdata/pending", out); err != nil {
		t.Fatal(err)
	}
	got := read(t, out)
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want one per pending snapshot", got)
	}
	// Sorted by path: jobs before session.
	if got[0].Test != job || got[0].Diff != snapdiff.ValuesChanged {
		t.Errorf("job = %+v, want values-changed", got[0])
	}
	if !strings.Contains(got[0].Message, `-  "state": "finished"`) || !strings.Contains(got[0].Message, `+  "state": "cancelled"`) {
		t.Errorf("job diff = %q", got[0].Message)
	}
	if got[1].Test != greeting || got[1].Diff != snapdiff.AdditiveOnly {
		t.Errorf("greeting = %+v, want additive-only", got[1])
	}
	if !strings.Contains(got[1].Message, `+    "metrics"`) {
		t.Errorf("greeting diff = %q", got[1].Message)
	}
	// insta's header records the assertion line; it is not the snapshot.
	for _, f := range got {
		if strings.Contains(f.Message, "assertion_line") {
			t.Errorf("metadata diffed: %q", f.Message)
		}
	}
}

func TestJUnitKeepsOtherFailures(t *testing.T) {
	out := filepath.Join(t.TempDir(), "snapshots.json")
	if err := run(101, "testdata/junit.xml", "testdata/workspace", "testdata/pending", out); err != nil {
		t.Fatal(err)
	}
	got := read(t, out)
	if len(got) != 3 {
		t.Fatalf("findings = %+v, want 2 snapshots and the broken pipe", got)
	}
	other := got[2]
	if other.Diff != "" || !strings.Contains(other.Test, "a_batch_worked_through") || !strings.Contains(other.Message, "BrokenPipe") {
		t.Errorf("non-snapshot failure = %+v", other)
	}
}

func TestNewSnapshot(t *testing.T) {
	pending := t.TempDir()
	testrepo.Write(t, pending, snaps+"service__new.snap.new", "---\nsource: x\n---\nhello\n")
	out := filepath.Join(t.TempDir(), "snapshots.json")
	if err := run(101, "", "testdata/workspace", pending, out); err != nil {
		t.Fatal(err)
	}
	got := read(t, out)
	if len(got) != 1 || got[0].Diff != snapdiff.AdditiveOnly || !strings.Contains(got[0].Message, "new snapshot") {
		t.Fatalf("got %+v", got)
	}
}

func TestEqualContentsAreNotAFinding(t *testing.T) {
	pending := t.TempDir()
	accepted, err := os.ReadFile(filepath.Join("testdata/workspace", job))
	if err != nil {
		t.Fatal(err)
	}
	// Only the header differs.
	testrepo.Write(t, pending, job+".new", strings.Replace(string(accepted), "expression:", "assertion_line: 3\nexpression:", 1))
	out := filepath.Join(t.TempDir(), "snapshots.json")
	if err := run(0, "", "testdata/workspace", pending, out); err != nil {
		t.Fatal(err)
	}
	if got := read(t, out); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestInlinePending(t *testing.T) {
	pending := t.TempDir()
	testrepo.Write(t, pending, "src/.lib.rs.pending-snap", "{}\n")
	out := filepath.Join(t.TempDir(), "snapshots.json")
	if err := run(101, "", "testdata/workspace", pending, out); err != nil {
		t.Fatal(err)
	}
	if got := read(t, out); len(got) != 1 || got[0].Diff != "" || !strings.Contains(got[0].Message, "inline") {
		t.Fatalf("got %+v", got)
	}
}

func TestRunErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	empty := t.TempDir()
	junitOnlyAsserts := filepath.Join(t.TempDir(), "junit.xml")
	testrepo.Write(t, filepath.Dir(junitOnlyAsserts), "junit.xml",
		`<testsuite><testcase name="a"><failure>snapshot assertion for 'a' failed in line 1</failure></testcase></testsuite>`)
	passing := filepath.Join(t.TempDir(), "junit.xml")
	testrepo.Write(t, filepath.Dir(passing), "junit.xml", `<testsuite><testcase name="a"/></testsuite>`)
	tests := []struct {
		name    string
		exit    int
		junit   string
		pending string
		want    int
		wantErr string
	}{
		{"clean run, no pending directory", 0, "", missing, 0, ""},
		{"clean run with junit", 0, passing, empty, 0, ""},
		{"failed with nothing pending", 101, "", empty, 0, "no pending snapshot"},
		{"build failed before junit", 101, filepath.Join(empty, "junit.xml"), empty, 0, "without a JUnit report"},
		// INSTA_UPDATE=no or a wrong INSTA_PENDING_DIR: insta failed the
		// assertions but wrote nothing to label.
		{"assertions without pending files", 101, junitOnlyAsserts, empty, 0, "INSTA_UPDATE=new"},
		{"failed with all tests passing", 101, passing, empty, 0, "no failing test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "findings.json")
			err := run(tt.exit, tt.junit, "testdata/workspace", tt.pending, out)
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
			if got := read(t, out); len(got) != tt.want {
				t.Fatalf("findings = %+v, want %d", got, tt.want)
			}
		})
	}
}

// The whole path: the adapter's file read by reliab verify, then shown by
// reliab report route with each label.
func TestRouteShowsLabels(t *testing.T) {
	findingsFile, err := filepath.Abs(filepath.Join(t.TempDir(), "snapshots.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := run(101, "", "testdata/workspace", "testdata/pending", findingsFile); err != nil {
		t.Fatal(err)
	}
	root := testrepo.New(t, `commands:
  snapshots:
    run: cp '`+filepath.ToSlash(findingsFile)+`' .reliab/out/snapshots.json
    format: findings
    output: .reliab/out/snapshots.json
`)
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if n, err := triage.Route(&buf, root); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v out=%q", n, err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"diff: " + snapdiff.Describe(snapdiff.AdditiveOnly),
		"diff: " + snapdiff.Describe(snapdiff.ValuesChanged),
		"--kind characterization-diff",
		`+  "state": "cancelled"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("route lacks %q:\n%s", want, out)
		}
	}
}
