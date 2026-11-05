package templates_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/hbaldwin98/reliab/internal/runner"
)

// The onboard skill ships copies of the examples, since a plugin cannot
// read files outside its own directory.
const skillExamples = "../plugin/skills/onboard/examples"

func examples(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("examples/*.reliab.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 3 {
		t.Fatalf("found %d examples, want at least 3 (Go, Rust, .NET)", len(paths))
	}
	return paths
}

// Source: README "reliab.yaml": every example loads, and every command
// runs in at least one context.
func TestExamplesLoad(t *testing.T) {
	for _, path := range examples(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			cfg, err := runner.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range cfg.Names() {
				c := cfg.Commands[name]
				if c.Change == "" && c.PR == "" && c.Audit == "" {
					t.Errorf("%s runs in no context", name)
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
			t.Fatalf("%s: the onboard skill lacks a copy: %v", filepath.Base(path), err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: the onboard skill's copy differs; copy templates/examples over it", filepath.Base(path))
		}
	}
	extra, _ := filepath.Glob(filepath.Join(skillExamples, "*"))
	if len(extra) != len(examples(t)) {
		t.Errorf("the onboard skill has %d examples, templates has %d", len(extra), len(examples(t)))
	}
}
