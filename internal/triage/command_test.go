package triage

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/verify"
)

// A mutation command that crashed has no mutant to kill: route gives the
// command-failure advice and names its log, not the surviving-mutant advice.
func TestRouteCommandFailure(t *testing.T) {
	root := testrepo.New(t, `commands:
  mutation_changed: echo tool crashed >&2; exit 1
`)
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Route(&buf, root); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "failed before reporting a result") || !strings.Contains(out, "prepr-mutation_changed.log") {
		t.Errorf("route = %q, want the command-failure advice and its log", out)
	}
	if strings.Contains(out, "find where the mutated behavior is intended") {
		t.Errorf("route gave mutant advice to a crashed command: %q", out)
	}
}

// Tools often end a failure with how to reproduce it; route keeps the end
// of a long message as well as its start.
func TestRouteKeepsMessageTail(t *testing.T) {
	root := testrepo.New(t, `commands:
  analyzers: >-
    for i in 1 2 3 4 5 6 7 8 9 10; do echo "line $i"; done;
    echo "Reproduce with: curl -X GET http://x/y"; exit 1
`)
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Route(&buf, root); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "curl -X GET http://x/y") || !strings.Contains(out, "\n  ...") || strings.Contains(out, "line 6\n") {
		t.Errorf("route = %q, want head, ..., and the reproducer at the tail", out)
	}
}

// After a failed build route names the commands it skipped, and only those
// (knowledge/decisions/verify-execution.md).
func TestRouteListsBuildFailedSkips(t *testing.T) {
	root := testrepo.New(t, `commands:
  build: exit 1
  unit: "true"
`)
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Route(&buf, root); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "skipped unit: the build failed") {
		t.Errorf("route = %q, want the skipped unit command", out)
	}
	if strings.Contains(out, "skipped corpus") || strings.Contains(out, "skipped properties") {
		t.Errorf("route lists skips the build did not cause: %q", out)
	}
}

func TestFirstLines(t *testing.T) {
	if got := firstLines("a\nb\nc", 4); got != "a\n  b\n  c" {
		t.Errorf("short message changed: %q", got)
	}
	// Cutting only the line before the tail would hide nothing, so it is kept.
	long := "1\n2\n3\n4\n5\n6\n7\n8"
	if got := firstLines(long, 4); got != "1\n  2\n  3\n  4\n  5\n  6\n  7\n  8" {
		t.Errorf("one-line cut not avoided: %q", got)
	}
	if got := firstLines(long+"\n9", 4); got != "1\n  2\n  3\n  4\n  ...\n  7\n  8\n  9" {
		t.Errorf("long message = %q", got)
	}
}
