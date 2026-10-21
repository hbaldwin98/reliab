package report

import "testing"

// Every kind is wired in full, so a new kind cannot ship half done: the
// SARIF rule text, the question a person is asked, and whether the agent may
// assign it all come from Kinds (knowledge/decisions/finding-classification.md).
func TestKindsAreFullyWired(t *testing.T) {
	consts := []string{
		KindUnclassified, KindNewCodeDefect, KindHarnessDefect, KindWrongProperty,
		KindLegacyDefectOrQuirk, KindCharacterizationDiff, KindEquivalentMutant,
		KindRequirementSilent, KindEnvironment, KindBelowThreshold,
	}
	seen := map[string]int{}
	for _, p := range Kinds {
		seen[p.Kind]++
		if p.Rule == "" {
			t.Errorf("%s: no rule text", p.Kind)
		}
		if p.Human != (p.Ask != "") {
			t.Errorf("%s: human %v but ask %q; a person kind needs a question and an agent kind has none", p.Kind, p.Human, p.Ask)
		}
	}
	for _, k := range consts {
		if seen[k] != 1 {
			t.Errorf("%s is in Kinds %d times, want once", k, seen[k])
		}
	}
	if len(Kinds) != len(consts) {
		t.Errorf("Kinds has %d rows, want %d", len(Kinds), len(consts))
	}
}

// Who resolves each kind: knowledge/concepts/finding-kind.md's table. Agent
// kinds come first, the order SARIF lists its rules in.
func TestKindActors(t *testing.T) {
	agent := []string{KindUnclassified, KindNewCodeDefect, KindHarnessDefect, KindWrongProperty}
	human := []string{KindLegacyDefectOrQuirk, KindCharacterizationDiff, KindEquivalentMutant, KindRequirementSilent, KindEnvironment, KindBelowThreshold}
	if !equal(AgentKinds, agent) || !equal(HumanKinds, human) {
		t.Errorf("AgentKinds %v, HumanKinds %v", AgentKinds, HumanKinds)
	}
	for _, k := range agent {
		if !NeedsAgent(Finding{Kind: k, Status: StatusOpen}) || NeedsHuman(Finding{Kind: k, Status: StatusOpen}) {
			t.Errorf("%s: an open finding must need the agent only", k)
		}
	}
	for _, k := range human {
		if !NeedsHuman(Finding{Kind: k, Status: StatusOpen}) || NeedsAgent(Finding{Kind: k, Status: StatusOpen}) {
			t.Errorf("%s: an open finding must need a person only", k)
		}
	}
	for _, st := range []string{StatusFixed, StatusDiscarded, StatusAcknowledged} {
		if NeedsAgent(Finding{Kind: KindNewCodeDefect, Status: st}) || NeedsHuman(Finding{Kind: KindEnvironment, Status: st}) {
			t.Errorf("a %s finding needs no one", st)
		}
	}
	if NeedsAgent(Finding{Kind: "no-such-kind", Status: StatusOpen}) || NeedsHuman(Finding{Kind: "no-such-kind", Status: StatusOpen}) {
		t.Error("an unknown kind needs no one")
	}
}

// The agent may assign every kind but two: adapters emit unclassified, and
// below-threshold is derived from the counts and never classified
// (knowledge/decisions/finding-classification.md, mutation-threshold.md).
func TestAssignableKinds(t *testing.T) {
	want := []string{
		KindNewCodeDefect, KindHarnessDefect, KindWrongProperty,
		KindLegacyDefectOrQuirk, KindCharacterizationDiff, KindEquivalentMutant,
		KindRequirementSilent, KindEnvironment,
	}
	if !equal(AssignableKinds, want) {
		t.Errorf("AssignableKinds = %v, want %v", AssignableKinds, want)
	}
	if _, ok := Policy("no-such-kind"); ok {
		t.Error("Policy found an unknown kind")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
