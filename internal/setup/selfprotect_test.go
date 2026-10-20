package setup

import (
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/doctor"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// knowledge/decisions/protected-path-approval.md: the profile guards its
// own thresholds, relevance rules, and baseline. Re-running init on a
// profile written before that adds the entry, keeping the file's
// formatting and comments, and says so.
func TestInitAddsSelfProtectionToAnExistingProfile(t *testing.T) {
	for name, tc := range map[string]struct{ protected, want string }{
		"flow list": {"protected_paths: [\"snapshots/**\"] # oracles\n",
			"protected_paths: [\"snapshots/**\", reliability-profile.yaml] # oracles\n"},
		"empty flow list": {"protected_paths: []\n",
			"protected_paths: [reliability-profile.yaml]\n"},
		"block list": {"# Oracles.\nprotected_paths:\n  - snapshots/**   # pinned\n  # golden files\n  - \"**/golden/**\"\n",
			"# Oracles.\nprotected_paths:\n  - snapshots/**   # pinned\n  # golden files\n  - \"**/golden/**\"\n  - reliability-profile.yaml\n"},
		"no key": {"", "protected_paths: [reliability-profile.yaml]\n"},
	} {
		t.Run(name, func(t *testing.T) {
			root := testrepo.New(t, tc.protected+"commands:\n  build: \"true\" # keep me\n")
			before := read(t, root, profile.DefaultPath)
			done, p, err := Init(root, Options{})
			if err != nil {
				t.Fatal(err)
			}
			after := read(t, root, profile.DefaultPath)
			if !profile.Protects(p, profile.DefaultPath) || !strings.Contains(strings.Join(done, "\n"), "added reliability-profile.yaml to protected_paths") {
				t.Errorf("done %q, protected %v", done, p.ProtectedPaths)
			}
			if !strings.Contains(after, tc.want) || !strings.Contains(after, "build: \"true\" # keep me\n") {
				t.Errorf("profile:\n%s", after)
			}
			// Nothing else in the file changed.
			if tc.protected != "" && strings.Replace(after, tc.want, tc.protected, 1) != before {
				t.Errorf("other lines changed:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if done, _, _ := Init(root, Options{}); strings.Contains(strings.Join(done, "\n"), "protected_paths") || read(t, root, profile.DefaultPath) != after {
				t.Errorf("a second init changed it again: %q", done)
			}
		})
	}
}

// The same Decision at the file's end: a block list that ends the file, and
// a last line with no newline, keep every line as it was.
func TestInitAddsSelfProtectionAtTheEndOfTheFile(t *testing.T) {
	for name, tc := range map[string]struct{ before, want string }{
		"block list last": {"commands:\n  build: \"true\"\nprotected_paths:\n  - snapshots/**\n",
			"commands:\n  build: \"true\"\nprotected_paths:\n  - snapshots/**\n  - reliability-profile.yaml\n"},
		"no final newline": {"commands:\n  build: \"true\"",
			"commands:\n  build: \"true\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			root := testrepo.New(t, tc.before)
			_, p, err := Init(root, Options{})
			if err != nil {
				t.Fatal(err)
			}
			after := read(t, root, profile.DefaultPath)
			if !profile.Protects(p, profile.DefaultPath) || !strings.Contains(after, tc.want) {
				t.Errorf("protected %v, profile:\n%s", p.ProtectedPaths, after)
			}
		})
	}
}

// A glob that already covers the profile is enough; init leaves it.
func TestInitKeepsACoveringGlob(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\"*.yaml\"]\ncommands:\n  build: \"true\"\n")
	before := read(t, root, profile.DefaultPath)
	if _, _, err := Init(root, Options{}); err != nil || read(t, root, profile.DefaultPath) != before {
		t.Errorf("profile changed: %v", err)
	}
}

// A shape init cannot edit safely is left alone, with a loud warning; doctor
// warns too until a person adds the entry.
func TestInitWarnsWhenItCannotEdit(t *testing.T) {
	root := testrepo.New(t, "protected_paths: [\n  \"snapshots/**\"\n]\ncommands:\n  build: \"true\"\n")
	before := read(t, root, profile.DefaultPath)
	done, _, err := Init(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, root, profile.DefaultPath) != before || !strings.Contains(strings.Join(done, "\n"), "WARNING: reliability-profile.yaml does not protect itself") {
		t.Errorf("done %q", done)
	}
	var warned bool
	for _, c := range doctor.Run(root) {
		if c.Name == "profile protection" {
			warned = c.Status == doctor.Warn && strings.Contains(c.Detail, "not in protected_paths")
		}
	}
	if !warned {
		t.Error("doctor does not warn about an unprotected profile")
	}
}
