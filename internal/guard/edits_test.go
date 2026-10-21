package guard

import (
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// protected_edits reminds on an edit that adds an auto-accept switch to test
// code: it would rewrite a protected snapshot at runtime, out of sight of
// every path rule. Text the file already had does not count.
func TestPreEditProtectedEdits(t *testing.T) {
	t.Parallel()
	r := open(t, "protected_paths: [\"**/*.verified.*\"]\nprotected_edits: ['\\bAutoVerify\\b']\ncommands:\n  build: \"true\"\n")
	testrepo.Write(t, r.Root, "test/Snap.cs", "Verify(x).AutoVerify();\n")
	edit := func(ti hookio.ToolInput) bool {
		d := r.PreEdit(hookio.Input{Cwd: r.Root, ToolInput: ti})
		if d.Deny != "" {
			t.Errorf("%s: denied: %s", ti.Path(), d.Deny)
		}
		return d.Context != ""
	}
	cases := []struct {
		name   string
		in     hookio.ToolInput
		remind bool
	}{
		{"Edit adds it", hookio.ToolInput{FilePath: "test/Other.cs", OldString: "Verify(x);", NewString: "Verify(x).AutoVerify();"}, true},
		{"Edit keeps an existing one", hookio.ToolInput{FilePath: "test/Snap.cs", OldString: "Verify(x).AutoVerify();", NewString: "Verify(y).AutoVerify();"}, false},
		{"Edit removes it", hookio.ToolInput{FilePath: "test/Snap.cs", OldString: "Verify(x).AutoVerify();", NewString: "Verify(x);"}, false},
		{"Write over a file that had one", hookio.ToolInput{FilePath: "test/Snap.cs", Content: "Verify(x).AutoVerify();\nmore();\n"}, false},
		{"Write a new file with one", hookio.ToolInput{FilePath: "test/New.cs", Content: "VerifierSettings.AutoVerify();"}, true},
		{"MultiEdit adds it", hookio.ToolInput{FilePath: "test/Other.cs", Edits: []hookio.TextEdit{{OldString: "a", NewString: "b"}, {OldString: "c", NewString: "AutoVerify()"}}}, true},
		{"NotebookEdit adds it", hookio.ToolInput{NotebookPath: "test/n.ipynb", NewSource: "AutoVerify()"}, true},
		{"unrelated edit", hookio.ToolInput{FilePath: "src/a/a.txt", OldString: "a", NewString: "b"}, false},
	}
	for _, c := range cases {
		if got := edit(c.in); got != c.remind {
			t.Errorf("%s: reminder=%v, want %v", c.name, got, c.remind)
		}
	}
	// Session start states the rule, as it does for protected_commands.
	if c := r.SessionStart(hookio.Input{}).Context; !strings.Contains(c, `; never add text matching \bAutoVerify\b (R2)`) {
		t.Errorf("session context does not list protected_edits: %q", c)
	}
}
