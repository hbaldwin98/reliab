package profile

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestShippedProfilesLoad(t *testing.T) {
	examples, err := filepath.Glob("../../templates/examples/*.profile.yaml")
	if err != nil || len(examples) == 0 {
		t.Fatalf("no example profiles: %v", err)
	}
	for _, path := range append([]string{"../../reliability-profile.yaml"}, examples...) {
		t.Run(path, func(t *testing.T) {
			p, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Roles) != 14 {
				t.Errorf("roles = %d, want 14", len(p.Roles))
			}
		})
	}
}

const minimal = `schemaVersion: 0.1.0
name: tiny
starting_condition: greenfield
verified_on: 2026-10-17
paths: { product: ["src/**"], tests: ["test/**"] }
roles:
  T1: { tool: go test }
  T2: { applicable: false, reason: none yet }
  T3: { applicable: false, reason: none yet }
  T4: { applicable: false, reason: none yet }
  T5: { applicable: false, reason: none yet }
  T6: { applicable: false, reason: none yet }
  T7: { applicable: false, reason: none yet }
  T8: { applicable: false, reason: none yet }
  T9: { applicable: false, reason: none yet }
  T10: { applicable: false, reason: none yet }
  T11: { applicable: false, reason: none yet }
  T12: { applicable: false, reason: none yet }
  T13: { applicable: false, reason: none yet }
  T14: { applicable: false, reason: none yet }
commands:
  build: go build ./...
  unit: { run: go test ./..., format: junit, output: out.xml }
`

func TestParseMinimal(t *testing.T) {
	p, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	if p.VerifiedOn != "2026-10-17" {
		t.Errorf("verified_on = %q; YAML timestamps must stay strings", p.VerifiedOn)
	}
	if got := p.Commands["build"]; got.Run != "go build ./..." || got.Format != "none" {
		t.Errorf("string shorthand decoded as %+v", got)
	}
	if got := p.Commands["unit"]; got.Format != "junit" || got.Output != "out.xml" {
		t.Errorf("object command decoded as %+v", got)
	}
}

func TestParseThresholdAndBaseline(t *testing.T) {
	src := strings.Replace(minimal, "name: tiny", "name: tiny\nprotected_paths: [\"mutation-baseline.json\"]\nmutation_baseline: mutation-baseline.json", 1)
	src = strings.Replace(src, "  unit:", "  mutation_changed: { run: x, threshold: 72.5 }\n  unit:", 1)
	p, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if c := p.Commands["mutation_changed"]; c.Threshold == nil || *c.Threshold != 72.5 || p.MutationBaseline != "mutation-baseline.json" {
		t.Errorf("decoded %+v, baseline %q", c, p.MutationBaseline)
	}
}

func TestParseRejections(t *testing.T) {
	tests := []struct {
		name, find, replace, want string
	}{
		{"unknown key", "name: tiny", "name: tiny\nflavour: x", "additional properties"},
		{"duplicate key", "name: tiny", "name: tiny\nname: again", "duplicate key"},
		{"bad starting condition", "greenfield", "brownfield", "starting_condition"},
		{"missing role", "  T14: { applicable: false, reason: none yet }\n", "", "T14"},
		{"role with neither shape", "T1: { tool: go test }", "T1: { notes: x }", "T1"},
		{"unknown command id", "  build:", "  lint:", "lint"},
		{"format without output", "format: junit, output: out.xml", "format: junit", "output"},
		{"future major", "schemaVersion: 0.1.0", "schemaVersion: 1.0.0", "supports major version 0"},
		{"bad protected command", "name: tiny", "name: tiny\nprotected_commands: ['cargo insta (accept']", "protected_commands[0]"},
		{"bad output glob", "format: junit, output: out.xml", "format: junit, output: 'out/[x.xml'", "not a valid glob"},
		{"bad protected edit", "name: tiny", "name: tiny\nprotected_edits: ['AutoVerify(']", "protected_edits[0]"},
		{"empty base", "name: tiny", "name: tiny\nbase: ''", "base"},
		{"changed_only not a bool", "format: junit, output: out.xml", "format: junit, output: out.xml, changed_only: yes-please", "changed_only"},
		// knowledge/decisions/mutation-threshold.md: only a mutation command has
		// a score, a threshold is a percentage, and the baseline is an oracle.
		{"threshold on a non-mutation command", "format: junit, output: out.xml", "format: junit, output: out.xml, threshold: 80", "only a mutation command"},
		{"threshold over 100", "  unit:", "  mutation_changed: { run: x, threshold: 101 }\n  unit:", "threshold"},
		// knowledge/decisions/relevance-skips.md: relevance is a list of globs.
		{"bad relevant_when glob", "format: junit, output: out.xml", "format: junit, output: out.xml, relevant_when: ['src/[']", "relevant_when"},
		{"empty relevant_when", "format: junit, output: out.xml", "format: junit, output: out.xml, relevant_when: []", "relevant_when"},
		{"baseline not protected", "name: tiny", "name: tiny\nmutation_baseline: mutation-baseline.json", "must match protected_paths"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(minimal, tt.find, tt.replace, 1)
			if src == minimal {
				t.Fatalf("test setup: %q not found", tt.find)
			}
			_, err := Parse([]byte(src))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

// knowledge/decisions/schema-v0-shape.md: a role is filled, not applicable,
// or needed but missing, each of the last two with a reason.
func TestRoleMissingShape(t *testing.T) {
	with := func(role string) string {
		return strings.Replace(minimal, "T5: { applicable: false, reason: none yet }", "T5: "+role, 1)
	}
	p, err := Parse([]byte(with("{ missing: true, reason: no in-process host yet }")))
	if err != nil {
		t.Fatal(err)
	}
	if r := p.Roles["T5"]; !r.Missing || r.Reason != "no in-process host yet" || r.Applicable != nil {
		t.Errorf("T5 = %+v", r)
	}
	for _, bad := range []string{
		"{ missing: true }",
		"{ missing: false, reason: x }",
		"{ missing: true, applicable: false, reason: x }",
		"{ missing: true, tool: x, reason: x }",
	} {
		if _, err := Parse([]byte(with(bad))); err == nil {
			t.Errorf("T5: %s accepted", bad)
		}
	}
}
