package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
)

// knowledge/decisions/consumer-contracts.md: contracts is a command id of
// its own in every tier, since consumer contract tests run against an
// in-process mock and cost about what unit tests do.
func TestContractsRunInEveryTier(t *testing.T) {
	root := repo(t, "commands:\n  contracts: echo ran > contracts.txt\n")
	for _, tier := range TierNames {
		_ = os.Remove(filepath.Join(root, "contracts.txt"))
		verify(t, root, tier)
		if _, err := os.Stat(filepath.Join(root, "contracts.txt")); err != nil {
			t.Errorf("%s: contracts did not run: %v", tier, err)
		}
	}
}

// knowledge/decisions/consumer-contracts.md: contracts depends on the
// optional role T16, so a profile that marks it not applicable skips
// contracts as such, and one that never mentions it still loads.
func TestContractsRoleT16(t *testing.T) {
	root := repo(t, "commands:\n  build: \"true\"\n")
	rep := verify(t, root, "inner")
	if s := skip(rep, "contracts"); s == nil || s.Reason != report.SkipNoCommand {
		t.Errorf("contracts skip without T16 = %+v, want no-command", s)
	}
	p := filepath.Join(root, "reliability-profile.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	prof := strings.Replace(string(b), "roles:\n", "roles:\n  T16: { applicable: false, reason: calls no service we own }\n", 1)
	if prof == string(b) {
		t.Fatal("the test profile has no roles line")
	}
	if err := os.WriteFile(p, []byte(prof), 0o644); err != nil {
		t.Fatal(err)
	}
	rep = verify(t, root, "inner")
	if s := skip(rep, "contracts"); s == nil || s.Reason != report.SkipNotApplicable || s.Detail != "T16: calls no service we own" {
		t.Errorf("contracts skip with T16 not applicable = %+v", s)
	}
	_, _, err = Run(context.Background(), Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner", Only: []string{"contracts"}})
	if err == nil || !strings.Contains(err.Error(), "T16 is not applicable") {
		t.Errorf("--only contracts: %v", err)
	}
}
