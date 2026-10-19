package doctor

import (
	"testing"

	"github.com/hbaldwin98/reliab/internal/testrepo"
)

func TestCommandWord(t *testing.T) {
	tests := []struct {
		run, want string
		ok        bool
	}{
		{"cargo build --workspace", "cargo", true},
		// A property command that sets the case count: the assignment is not the program.
		{"PROPTEST_CASES={cases} cargo nextest run", "cargo", true},
		{"A=1 B=2 go test ./...", "go", true},
		{"$TOOL run", "", false},
		{"A=1 `which go` test", "", false},
		{"A=1", "", false},
		{"", "", false},
		{"scripts/mutation.sh changed", "scripts/mutation.sh", true},
	}
	for _, tt := range tests {
		got, ok := commandWord(tt.run)
		if got != tt.want || ok != tt.ok {
			t.Errorf("commandWord(%q) = %q, %v; want %q, %v", tt.run, got, ok, tt.want, tt.ok)
		}
	}
}

// knowledge/decisions/protected-path-approval.md: a profile that does not
// protect itself is a warning; one that does is not mentioned.
func TestDoctorWarnsWhenTheProfileIsUnprotected(t *testing.T) {
	check := func(extra string) *Check {
		t.Helper()
		for _, c := range Run(testrepo.New(t, extra+"commands:\n  build: \"true\"\n")) {
			if c.Name == "profile protection" {
				return &c
			}
		}
		return nil
	}
	if c := check(""); c == nil || c.Status != Warn {
		t.Errorf("unprotected profile: %+v", c)
	}
	if c := check("protected_paths: [reliability-profile.yaml]\n"); c != nil {
		t.Errorf("protected profile: %+v", c)
	}
}
