package characterize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/verify"
	"github.com/hbaldwin98/reliab/pkg/snapdiff"
)

// The cycle's intent is knowledge/decisions/on-demand-characterization.md:
// capture an area, change it, compare, discard; differences are findings
// labelled as snapshot diffs are.

// copyScope is a characterize command for tests: its "transcript" of each
// file under the scope is the file itself.
const copyScope = `commands:
  characterize: for p in $RELIAB_SCOPE; do find "$p" -type f | while read -r f; do mkdir -p "{capture}/$(dirname "$f")"; cp "$f" "{capture}/$f"; done; done
`

func opts(root string, scope ...string) verify.Options {
	return verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "inner", Scope: scope}
}

func TestDiff(t *testing.T) {
	base, cur := t.TempDir(), t.TempDir()
	testrepo.Write(t, base, "same.txt", "x\n")
	testrepo.Write(t, cur, "same.txt", "x\n")
	testrepo.Write(t, base, "grew.txt", "x\n")
	testrepo.Write(t, cur, "grew.txt", "x\ny\n")
	testrepo.Write(t, base, "changed.txt", "x\n")
	testrepo.Write(t, cur, "changed.txt", "z\n")
	testrepo.Write(t, base, "gone/old.txt", "x\n")
	testrepo.Write(t, cur, "new.txt", "y\n")
	testrepo.Write(t, base, "empty-gone.txt", "")
	testrepo.Write(t, cur, "empty-new.txt", "")
	fs, err := Diff(base, cur)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"changed.txt":    snapdiff.ValuesChanged,
		"empty-gone.txt": snapdiff.ValuesChanged,
		"empty-new.txt":  snapdiff.AdditiveOnly,
		"gone/old.txt":   snapdiff.ValuesChanged,
		"grew.txt":       snapdiff.AdditiveOnly,
		"new.txt":        snapdiff.AdditiveOnly,
	}
	if len(fs) != len(want) {
		t.Fatalf("findings = %+v, want %d", fs, len(want))
	}
	for _, f := range fs {
		if want[f.Test] != f.Diff {
			t.Errorf("%s: diff %q, want %q", f.Test, f.Diff, want[f.Test])
		}
		if f.Command != verify.CharacterizeID || f.Kind != report.KindUnclassified || f.Status != report.StatusOpen {
			t.Errorf("%s: %+v", f.Test, f)
		}
	}
	if !strings.Contains(fs[3].Message, "missing from the capture") {
		t.Errorf("gone file message = %q", fs[3].Message)
	}
}

func TestCompareWithoutCapture(t *testing.T) {
	root := testrepo.New(t, copyScope)
	if _, _, err := Compare(context.Background(), opts(root)); err == nil || !strings.Contains(err.Error(), "capture before the change") {
		t.Fatalf("err = %v", err)
	}
}

func TestCaptureFailureLeavesNoBaseline(t *testing.T) {
	for name, cmd := range map[string]string{
		"exit":    "commands:\n  characterize: echo boom; exit 3\n",
		"nothing": "commands:\n  characterize: \"true\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := testrepo.New(t, cmd)
			rep, _, err := Capture(context.Background(), opts(root))
			if err != nil {
				t.Fatal(err)
			}
			if !rep.HasOpen() || rep.Findings[0].Command != verify.CharacterizeID {
				t.Fatalf("findings = %+v", rep.Findings)
			}
			if _, err := os.Stat(filepath.Join(root, Dir)); !os.IsNotExist(err) {
				t.Fatalf("baseline kept after a failed capture: %v", err)
			}
		})
	}
}

func TestCompareFailureIsAFinding(t *testing.T) {
	root := testrepo.New(t, copyScope)
	if _, _, err := Capture(context.Background(), opts(root, "src/a")); err != nil {
		t.Fatal(err)
	}
	testrepo.Write(t, root, "reliability-profile.yaml", strings.Replace(mustRead(t, filepath.Join(root, "reliability-profile.yaml")), copyScope, "commands:\n  characterize: exit 4\n", 1))
	rep, _, err := Compare(context.Background(), opts(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 1 || !strings.Contains(rep.Findings[0].Message, "exit 4") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

// Only reliab characterize runs the characterize command: elsewhere
// {capture} would name nothing.
func TestVerifyRefusesCharacterize(t *testing.T) {
	root := testrepo.New(t, copyScope+"tiers:\n  inner: { run: [characterize] }\n")
	for _, opt := range []verify.Options{{Tier: "prepr", Only: []string{"characterize"}}, {Tier: "inner"}} {
		opt.Root, opt.Profile = root, "reliability-profile.yaml"
		if _, _, err := verify.Run(context.Background(), opt); err == nil || !strings.Contains(err.Error(), "reliab characterize") {
			t.Errorf("%+v: err = %v", opt, err)
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
