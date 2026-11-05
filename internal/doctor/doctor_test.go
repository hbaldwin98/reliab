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

// Source: README "Install" and "reliab.yaml": doctor fails without a valid
// reliab.yaml and passes with one.
func TestDoctorReadsReliabYAML(t *testing.T) {
	find := func(cs []Check, name string) *Check {
		for _, c := range cs {
			if c.Name == name {
				return &c
			}
		}
		return nil
	}
	cs := Run(testrepo.New(t, ""))
	if c := find(cs, "reliab.yaml"); c == nil || c.Status != Fail || !Failed(cs) {
		t.Fatalf("no reliab.yaml: %+v", cs)
	}
	cs = Run(testrepo.New(t, "version: 1\ncommands:\n  build: { about: x, pr: \"true\" }\n"))
	if c := find(cs, "reliab.yaml"); c == nil || c.Status != OK || c.Detail != "1 commands" {
		t.Fatalf("valid reliab.yaml: %+v", cs)
	}
	if c := find(cs, ".gitignore"); c == nil || c.Status != OK {
		t.Fatalf(".gitignore: %+v", c)
	}
}
