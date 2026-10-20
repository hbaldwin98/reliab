package guard

import (
	"context"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
	"github.com/hbaldwin98/reliab/internal/verify"
)

// Every rule tested here is stated in knowledge/decisions/named-tests.md.

const needsTests = "protected_paths: [\"snapshots/**\"]\ncommands:\n  build: \"true\"\n  store_tests:\n    run: \"true\"\n    needs: [tests]\n" +
	"tiers:\n  inner: { run: [build, store_tests] }\n  prepr: { run: [build, store_tests] }\n"

func runNamed(t *testing.T, r *Repo, tier string, tests ...string) {
	t.Helper()
	opt := verify.Options{Root: r.Root, Profile: "reliability-profile.yaml", Tier: tier, Tests: tests}
	if _, _, err := verify.Run(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
}

// A tier run that names its tests is a normal tier run for both hooks; one
// that needed tests and named none keeps the stop hook asking and the push
// denied, saying which command waits.
func TestHooksWaitForNamedTests(t *testing.T) {
	r := open(t, needsTests)
	for _, tier := range []string{"inner", "prepr"} {
		runNamed(t, r, tier)
	}
	if d := r.Stop(hookio.Input{}); !strings.Contains(d.Block, "store_tests: no tests named") {
		t.Errorf("stop with tests unnamed: %+v", d)
	}
	if d := r.Stop(hookio.Input{StopHookActive: true}); d.Block != "" {
		t.Error("blocked although stop_hook_active is set")
	}
	if d := r.PrePush(); !strings.Contains(d.Deny, "store_tests: no tests named") {
		t.Errorf("push with tests unnamed: %+v", d)
	}

	for _, tier := range []string{"inner", "prepr"} {
		runNamed(t, r, tier, "--test A")
	}
	if d := r.Stop(hookio.Input{}); d.Block != "" {
		t.Errorf("stop after a run that named its tests: %s", d.Block)
	}
	if d := r.PrePush(); d.Deny != "" {
		t.Errorf("push after a prepr run that named its tests: %s", d.Deny)
	}
}
