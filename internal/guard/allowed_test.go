package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
)

// Lines the review found denied though they write nothing protected and run
// no person-only command. A false positive costs as much as a bypass
// (the user's priority for this workstream; retro 2026-10-20).
func TestPreBashReviewAllowed(t *testing.T) {
	t.Parallel()
	r := open(t, "protected_paths: [\"snapshots/**\", \"reliability-profile.yaml\"]\ncommands:\n  build: \"true\"\n")
	other := scratch(t)
	sample := filepath.Join(other, "sample")
	if err := os.MkdirAll(sample, 0o755); err != nil {
		t.Fatal(err)
	}
	s := filepath.ToSlash(sample)
	for _, cmd := range []string{
		// Writing a sample repository's profile with a heredoc, or with
		// non-ASCII text, in a directory outside this repository (retro
		// 2026-10-20: a throwaway sample repo is not this repository).
		"cd " + s + " && cat > reliability-profile.yaml <<'EOF'\nname: sample\nEOF",
		"cd " + s + " && cat > reliability-profile.yaml <<EOF\nname: it's a sample\nEOF",
		"cd " + s + " && echo café > reliability-profile.yaml",
		// A commit message names reliab approve or findings ack; the line
		// also pushes, pipes, or tests (retro 2026-10-20: commit text is
		// data). The push itself still meets the gate, so the first is
		// judged by its denial's reason.
		`git commit -q -m "Guard: reliab findings ack is for people" || true`,
		`git commit -m "reliab approve" && go test ./...`,
	} {
		d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}})
		if d.Deny != "" {
			t.Errorf("%q: denied (%s)", cmd, d.Deny)
		}
	}
	push := `git add -A && git commit -m "Docs: reliab approve is for people" && git push -u origin feat`
	if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: push}}); strings.Contains(d.Deny, "approv") {
		t.Errorf("%q: denied as a person-only command (%s)", push, d.Deny)
	}
	// Still denied: the heredoc writes this repository's oracle,
	// and a line that runs the stored message as code judges it whole.
	for _, cmd := range []string{
		"cat > reliability-profile.yaml <<'EOF'\nname: x\nEOF",
		"cd " + s + " && cat > " + filepath.ToSlash(filepath.Join(r.Root, "snapshots", "s.txt")) + " <<'EOF'\nx\nEOF",
		`git commit -m "reliab approve x" && bash -c "$(git log -1 --format=%B)"`,
		`git commit -m "x" && reliab approve x`,
		`git commit -m "reliab approve x" && go test -exec "$(git log -1 --format=%B)" ./...`,
		`echo "reliab approve x"; git`,
	} {
		if d := r.PreBash(hookio.Input{ToolName: "Bash", Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny == "" {
			t.Errorf("%q: allowed", cmd)
		}
	}
}
