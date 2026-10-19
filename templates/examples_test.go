package templates_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/hbaldwin98/reliab/internal/profile"
)

// The onboard skill ships copies of the examples, since a plugin cannot
// read files outside its own directory.
const skillExamples = "../plugin/skills/onboard/examples"

// commandRole is the role each command depends on (internal/verify), so an
// example never wires a command for a role it marks not applicable.
var commandRole = map[string]string{
	"unit": "T1", "properties": "T2", "model_tests": "T2", "snapshots": "T3",
	"mutation_changed": "T4", "mutation_full": "T4", "api_fuzz": "T7",
	"faults_http": "T9", "faults_db": "T10", "store_tests": "T11",
	"coverage": "T12", "analyzers": "T14",
}

func examples(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("examples/*.profile.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 3 {
		t.Fatalf("found %d example profiles, want at least 3 (Go, Rust, .NET)", len(paths))
	}
	return paths
}

func TestExampleProfilesAreValid(t *testing.T) {
	for _, path := range examples(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			p, err := profile.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			for id, role := range p.Roles {
				switch {
				case role.Applicable != nil:
				case role.Verified == "":
					t.Errorf("%s (%s) has no verified date: an example shows a finished S0", id, role.Tool)
				}
			}
			for id := range p.Commands {
				role, ok := commandRole[id]
				if !ok {
					continue
				}
				if r := p.Roles[role]; r.Applicable != nil {
					t.Errorf("command %s is wired but %s is not applicable (%s)", id, role, r.Reason)
				}
			}
			for name, tier := range p.Tiers {
				for _, id := range tier.Run {
					if _, ok := p.Commands[id]; !ok {
						t.Errorf("tiers.%s runs %s, which commands does not define", name, id)
					}
				}
			}
		})
	}
}

func TestSkillExamplesMatch(t *testing.T) {
	for _, path := range examples(t) {
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(skillExamples, filepath.Base(path)))
		if err != nil {
			t.Errorf("%v: copy it from templates/examples", err)
			continue
		}
		if !bytes.Equal(normalize(got), normalize(want)) {
			t.Errorf("%s differs from %s: copy it from templates/examples", filepath.Join(skillExamples, filepath.Base(path)), path)
		}
	}
	copies, _ := filepath.Glob(filepath.Join(skillExamples, "*"))
	if len(copies) != len(examples(t)) {
		t.Errorf("%s holds %d files, want one per example", skillExamples, len(copies))
	}
}

// normalize ignores line endings, which git may convert on checkout.
func normalize(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }
