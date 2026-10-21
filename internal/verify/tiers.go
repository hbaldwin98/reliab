package verify

import "github.com/hbaldwin98/reliab/internal/profile"

// Tier is a resolved tier: built-in defaults overlaid with the profile.
type Tier struct {
	Name          string
	BudgetSeconds int // 0 means no budget
	PropertyCases int
	// FuzzSeconds is how long the fuzz command explores; 0 replays only
	// its saved corpus (knowledge/decisions/fuzz-and-e2e.md).
	FuzzSeconds int
	Run         []string // command ids, in order
}

// defaults is the paper's Figure 1 as command lists. Cheap commands run in
// every tier; open-ended ones join at prepr and run fully at night. In the
// inner tier {impacted} narrows tests to the changed code. Integration and
// end-to-end tests (a host, a real store or service, the whole system) are
// too slow for the edit loop, so they join at prepr, as does fuzzing, with a
// short time box there and a long one at night.
var defaults = map[string]Tier{
	"inner": {
		Name: "inner", BudgetSeconds: 300, PropertyCases: 100,
		Run: []string{"build", "analyzers", "corpus", "unit", "properties", "spec_checks"},
	},
	"prepr": {
		Name: "prepr", BudgetSeconds: 900, PropertyCases: 1000, FuzzSeconds: 120,
		Run: []string{
			"build", "analyzers", "corpus", "unit", "integration_tests", "e2e", "snapshots", "properties",
			"spec_checks", "model_tests", "api_fuzz", "fuzz", "faults_db", "faults_http", "store_tests",
			"mutation_changed", "replay_sampled", "coverage",
		},
	},
	"nightly": {
		Name: "nightly", BudgetSeconds: 0, PropertyCases: 10000, FuzzSeconds: 1800,
		Run: []string{
			"build", "analyzers", "corpus", "unit", "integration_tests", "e2e", "snapshots", "properties",
			"spec_checks", "model_tests", "api_fuzz", "fuzz", "faults_db", "faults_http", "store_tests",
			"mutation_full", "replay_full", "coverage",
		},
	},
}

// TierNames lists the tiers verify accepts.
var TierNames = []string{"inner", "prepr", "nightly"}

// ResolveTier returns the named tier with the profile's overrides applied.
// ok is false for an unknown tier name.
func ResolveTier(p *profile.Profile, name string) (Tier, bool) {
	t, ok := defaults[name]
	if !ok {
		return Tier{}, false
	}
	t.Run = append([]string(nil), t.Run...)
	if o, ok := p.Tiers[name]; ok {
		if o.BudgetSeconds != nil {
			t.BudgetSeconds = *o.BudgetSeconds
		}
		if o.PropertyCases != nil {
			t.PropertyCases = *o.PropertyCases
		}
		if o.FuzzSeconds != nil {
			t.FuzzSeconds = *o.FuzzSeconds
		}
		if o.Run != nil {
			t.Run = append([]string(nil), o.Run...)
		}
	}
	return t, true
}

// commandRole names the tool role a command depends on, for those that
// depend on one (unit only on T1; build, corpus, integration_tests, and e2e
// on none), so a missing command whose role is marked not applicable is
// skipped as such rather than as "no command".
var commandRole = map[string]string{
	"unit":             "T1",
	"properties":       "T2",
	"model_tests":      "T2",
	"snapshots":        "T3",
	"mutation_changed": "T4",
	"mutation_full":    "T4",
	"api_fuzz":         "T7",
	"fuzz":             "T15",
	"faults_http":      "T9",
	"faults_db":        "T10",
	"store_tests":      "T11",
	"coverage":         "T12",
	"analyzers":        "T14",
}
