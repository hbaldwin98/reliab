package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

// knowledge/decisions/fuzz-and-e2e.md: fuzz and e2e are command ids of their
// own that join at prepr, as integration_tests do, and fuzz explores for the
// tier's fuzz_seconds: 120 at prepr and 1800 at night by default, a profile
// override winning, passed as {fuzz_seconds} and RELIAB_FUZZ_SECONDS.
func TestFuzzAndE2ERunFromPreprWithTheTiersTimeBox(t *testing.T) {
	root := repo(t, `commands:
  fuzz: echo "{fuzz_seconds} $RELIAB_FUZZ_SECONDS" > fuzz.txt
  e2e: echo ran > e2e.txt
tiers:
  nightly: { fuzz_seconds: 45 }
`)
	for _, c := range []struct {
		tier, fuzz string
		e2e        bool
	}{
		{"inner", "", false},
		{"prepr", "120 120", true},
		{"nightly", "45 45", true},
	} {
		for _, f := range []string{"fuzz.txt", "e2e.txt"} {
			_ = os.Remove(filepath.Join(root, f))
		}
		verify(t, root, c.tier)
		got, _ := os.ReadFile(filepath.Join(root, "fuzz.txt"))
		if strings.TrimSpace(string(got)) != c.fuzz {
			t.Errorf("%s: fuzz wrote %q, want %q", c.tier, got, c.fuzz)
		}
		if _, err := os.Stat(filepath.Join(root, "e2e.txt")); (err == nil) != c.e2e {
			t.Errorf("%s: e2e ran %v, want %v", c.tier, err == nil, c.e2e)
		}
	}
}

// knowledge/decisions/fuzz-and-e2e.md: fuzz depends on the optional role
// T15, so a profile that marks it not applicable skips fuzz as such, and one
// that never mentions it still loads.
func TestFuzzRoleT15(t *testing.T) {
	root := repo(t, "commands:\n  build: \"true\"\n")
	rep := verify(t, root, "prepr")
	if s := skip(rep, "fuzz"); s == nil || s.Reason != report.SkipNoCommand {
		t.Errorf("fuzz skip without T15 = %+v, want no-command", s)
	}
	p := filepath.Join(root, "reliability-profile.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	prof := strings.Replace(string(b), "roles:\n", "roles:\n  T15: { applicable: false, reason: no parsers or decoders }\n", 1)
	if prof == string(b) {
		t.Fatal("the test profile has no roles line")
	}
	if err := os.WriteFile(p, []byte(prof), 0o644); err != nil {
		t.Fatal(err)
	}
	rep = verify(t, root, "prepr")
	if s := skip(rep, "fuzz"); s == nil || s.Reason != report.SkipNotApplicable || s.Detail != "T15: no parsers or decoders" {
		t.Errorf("fuzz skip with T15 not applicable = %+v", s)
	}
	_, _, err = Run(context.Background(), Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr", Only: []string{"fuzz"}})
	if err == nil || !strings.Contains(err.Error(), "T15 is not applicable") {
		t.Errorf("--only fuzz: %v", err)
	}
}
