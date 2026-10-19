package doctor

import "testing"

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
