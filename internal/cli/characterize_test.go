package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/characterize"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// The cycle the characterize skill drives
// (knowledge/decisions/on-demand-characterization.md): capture, change,
// compare, discard. The fixture's characterize command transcribes each
// file under the scope by copying it.
func TestCharacterizeCycle(t *testing.T) {
	root := testrepo.New(t, `commands:
  characterize: for p in $RELIAB_SCOPE; do find "$p" -type f | while read -r f; do mkdir -p "{capture}/$(dirname "$f")"; cp "$f" "{capture}/$f"; done; done
`)
	testrepo.Write(t, root, "src/a/greeting.txt", "hello\nversion 3\n")
	testrepo.Write(t, root, "src/a/pane.txt", "state running\nstate exited\n")
	t.Chdir(root)

	code, out, errOut := run("characterize", "capture", "--scope", "src/a")
	if code != ExitOK || !strings.Contains(out, "captured 3 file(s)") {
		t.Fatalf("capture: exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	// A refactor that changes nothing.
	if code, out, errOut := run("characterize", "compare"); code != ExitOK || !strings.Contains(out, "no differences") {
		t.Fatalf("unchanged compare: exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	// One addition and one changed value.
	testrepo.Write(t, root, "src/a/greeting.txt", "hello\nversion 3\ncapability decisions\n")
	testrepo.Write(t, root, "src/a/pane.txt", "state running\nstate killed\n")
	code, out, errOut = run("characterize", "compare")
	if code != ExitFindings || !strings.Contains(errOut, "2 difference(s)") {
		t.Fatalf("changed compare: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	// Capturing again now would make the changed behavior the baseline.
	if code, _, errOut := run("characterize", "capture", "--scope", "src/a"); code != ExitConfig || !strings.Contains(errOut, "already exists") {
		t.Fatalf("re-capture: exit %d, stderr %q", code, errOut)
	}
	_, route, _ := run("report", "route")
	for _, want := range []string{
		"src/a/greeting.txt", "diff: additive only", "+capability decisions",
		"src/a/pane.txt", "diff: values changed or removed", "-state exited", "+state killed",
	} {
		if !strings.Contains(route, want) {
			t.Errorf("route lacks %q:\n%s", want, route)
		}
	}

	// Compare again, after the change is put right: the capture is kept.
	testrepo.Write(t, root, "src/a/pane.txt", "state running\nstate exited\n")
	if code, _, errOut := run("characterize", "compare"); code != ExitFindings || !strings.Contains(errOut, "1 difference(s)") {
		t.Fatalf("second compare: exit %d, stderr %q", code, errOut)
	}

	if code, _, _ := run("characterize", "discard"); code != ExitOK {
		t.Fatalf("discard: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(root, characterize.Dir)); !os.IsNotExist(err) {
		t.Fatalf("captures left after discard: %v", err)
	}
	if code, _, errOut := run("characterize", "compare"); code != ExitConfig || !strings.Contains(errOut, "capture before the change") {
		t.Fatalf("compare after discard: exit %d, stderr %q", code, errOut)
	}

	// The captures stayed under .reliab/, which git ignores: the only
	// untracked files are the fixture's own.
	st, err := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(st) != "?? src/a/greeting.txt\n?? src/a/pane.txt\n" {
		t.Errorf("git status = %q", st)
	}
}

// knowledge/decisions/named-tests.md: a command whose profile needs tests,
// run without them, is skipped with reason needs-input and the run is
// incomplete for that reason, not the budget. compare says which, as
// verify does.
func TestCharacterizeCompareNamesWhyItIsIncomplete(t *testing.T) {
	const transcribe = `for p in $RELIAB_SCOPE; do find "$p" -type f | while read -r f; do mkdir -p "{capture}/$(dirname "$f")"; cp "$f" "{capture}/$f"; done; done`
	root := testrepo.New(t, "commands:\n  characterize: '"+transcribe+"'\n")
	t.Chdir(root)
	if code, out, errOut := run("characterize", "capture", "--scope", "src/a"); code != ExitOK {
		t.Fatalf("capture: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	prof, err := os.ReadFile(filepath.Join(root, "reliability-profile.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Write(t, root, "reliability-profile.yaml", strings.Replace(string(prof), "  characterize: '", "  characterize:\n    needs: [tests]\n    run: '", 1))
	testrepo.Write(t, root, "src/a/new.txt", "a change")
	code, out, errOut := run("characterize", "compare")
	if code != ExitIncomplete || !strings.Contains(errOut, "characterize: no tests named") || strings.Contains(errOut, "budget") {
		t.Errorf("compare waiting for tests: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}
