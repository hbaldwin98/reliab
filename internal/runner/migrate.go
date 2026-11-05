package runner

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// OldProfilePath is the v1 profile that Migrate reads.
const OldProfilePath = "reliability-profile.yaml"

// oldTiers are v1's default tier command lists, mapped to contexts
// (knowledge/decisions/profile-v2.md: contexts replace tiers).
var oldTiers = map[string][]string{
	"change": {"build", "analyzers", "corpus", "unit", "properties", "spec_checks", "contracts"},
	"pr": {"build", "analyzers", "corpus", "unit", "integration_tests", "e2e", "snapshots", "properties",
		"spec_checks", "contracts", "model_tests", "api_fuzz", "fuzz", "faults_db", "faults_http", "store_tests",
		"mutation_changed", "replay_sampled", "coverage"},
	"audit": {"build", "analyzers", "corpus", "unit", "integration_tests", "e2e", "snapshots", "properties",
		"spec_checks", "contracts", "model_tests", "api_fuzz", "fuzz", "faults_db", "faults_http", "store_tests",
		"mutation_full", "replay_full", "coverage"},
}

var tierContext = map[string]string{"inner": "change", "prepr": "pr", "nightly": "audit"}

// v1 per-tier inputs, for the {cases} and {fuzz_seconds} placeholders.
var defaultCases = map[string]int{"change": 100, "pr": 1000, "audit": 10000}
var defaultFuzz = map[string]int{"change": 0, "pr": 120, "audit": 1800}

// merged are v1 command pairs that become one command, one line per context.
var merged = map[string]string{
	"mutation_changed": "mutation", "mutation_full": "mutation",
	"replay_sampled": "replay", "replay_full": "replay",
}

// abouts are starting descriptions for v1's well-known commands. A person
// should sharpen each for their project.
var abouts = map[string]string{
	"build":             "Compile everything. If this fails, nothing else is worth running.",
	"analyzers":         "Static analysis. Each issue is a finding at a file and line: fix the code, never silence the analyzer to pass.",
	"unit":              "Unit tests. A failing test is a product bug (fix the code) or a wrong test (fix the test; never loosen an assertion).",
	"properties":        "Property tests: rules that must hold for any input. A failure prints a shrunk counterexample; reproduce it from its seed.",
	"integration_tests": "Integration tests against real dependencies.",
	"e2e":               "End-to-end tests through the whole system.",
	"snapshots":         "Snapshot tests. A changed snapshot is either a bug or an intended change a person accepts.",
	"contracts":         "Consumer contract tests: does this service still honor what its consumers expect?",
	"mutation":          "Mutation testing. A surviving mutant means no test checks that line's behavior: add a test that does, or say why none can.",
	"coverage":          "Coverage: which lines the tests reach.",
	"fuzz":              "Fuzzing: random and malformed input. A crash is saved as a reproducible case.",
	"api_fuzz":          "API fuzzing against the running service.",
	"faults_db":         "Fault injection on the data store.",
	"faults_http":       "Fault injection on HTTP dependencies.",
	"store_tests":       "Tests against the real data store.",
	"spec_checks":       "Specification checks.",
	"model_tests":       "Model-based tests.",
	"corpus":            "Replays saved failing inputs.",
	"replay":            "Replays recorded traffic.",
}

// heavy commands claim the machine.
var heavy = map[string]bool{"mutation": true, "fuzz": true, "api_fuzz": true}

type oldCommand struct {
	Run       string   `yaml:"run"`
	Format    string   `yaml:"format"`
	Output    string   `yaml:"output"`
	Threshold *float64 `yaml:"threshold"`
}

func (c *oldCommand) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		c.Run = n.Value
		return nil
	}
	type plain oldCommand
	return n.Decode((*plain)(c))
}

type oldProfile struct {
	Base     string                `yaml:"base"`
	Commands map[string]oldCommand `yaml:"commands"`
	Impacted *struct {
		All string `yaml:"all"`
	} `yaml:"impacted"`
	Tiers map[string]struct {
		Run           []string `yaml:"run"`
		PropertyCases *int     `yaml:"property_cases"`
		FuzzSeconds   *int     `yaml:"fuzz_seconds"`
	} `yaml:"tiers"`
	Debug *yaml.Node `yaml:"debug"`
}

// Migration is what Migrate produces.
type Migration struct {
	// Config is the reliab.yaml text.
	Config string
	// Debug is the reliab-debug.yaml text, empty when v1 had no debug section.
	Debug string
	// Notes are what a person should check.
	Notes []string
}

var oldPlaceholderRe = regexp.MustCompile(`\{([a-z_]+)\}`)

// Migrate converts a v1 reliability-profile.yaml. Roles, tiers' budgets,
// protected paths, relevance rules and corrections have no v2 form and are
// dropped, with a note.
func Migrate(raw []byte) (*Migration, error) {
	var old oldProfile
	if err := yaml.Unmarshal(raw, &old); err != nil {
		return nil, fmt.Errorf("%s: %w", OldProfilePath, err)
	}
	if len(old.Commands) == 0 {
		return nil, fmt.Errorf("%s has no commands", OldProfilePath)
	}
	m := &Migration{}
	note := func(f string, a ...any) { m.Notes = append(m.Notes, fmt.Sprintf(f, a...)) }

	tiers := map[string][]string{}
	cases, fuzz := map[string]int{}, map[string]int{}
	for _, ctx := range Contexts {
		tiers[ctx], cases[ctx], fuzz[ctx] = oldTiers[ctx], defaultCases[ctx], defaultFuzz[ctx]
	}
	for tier, t := range old.Tiers {
		ctx := tierContext[tier]
		if ctx == "" {
			continue
		}
		if t.Run != nil {
			tiers[ctx] = t.Run
		}
		if t.PropertyCases != nil {
			cases[ctx] = *t.PropertyCases
		}
		if t.FuzzSeconds != nil {
			fuzz[ctx] = *t.FuzzSeconds
		}
	}

	cmds := map[string]*Command{}
	for _, ctx := range Contexts {
		for _, id := range tiers[ctx] {
			oc, ok := old.Commands[id]
			if !ok || oc.Run == "" {
				continue
			}
			name := id
			if n, ok := merged[id]; ok {
				name = n
			}
			c := cmds[name]
			if c == nil {
				c = &Command{Name: name, About: abouts[name]}
				if c.About == "" {
					c.About = "TODO: say what " + name + " checks and what its failure means."
					note("commands.%s: write its about", name)
				}
				if oc.Format != "" && oc.Format != "none" && oc.Output != "" {
					c.Output = &Output{File: oc.Output, Parse: oc.Format}
				}
				if oc.Threshold != nil {
					c.Threshold = *oc.Threshold
				}
				c.Exclusive = heavy[name]
				cmds[name] = c
			}
			line := convertLine(oc.Run, ctx, old, cases[ctx], fuzz[ctx], note, name)
			switch ctx {
			case "change":
				c.Change = line
			case "pr":
				c.PR = line
			case "audit":
				c.Audit = line
			}
		}
	}
	for id := range old.Commands {
		name := id
		if n, ok := merged[id]; ok {
			name = n
		}
		if cmds[name] == nil {
			note("commands.%s ran in no tier, so it is not migrated", id)
		}
	}

	// after: everything after build; mutation and coverage after unit.
	for name, c := range cmds {
		switch {
		case name == "build":
		case (name == "mutation" || name == "coverage") && cmds["unit"] != nil:
			c.After = []string{"unit"}
		case cmds["build"] != nil:
			c.After = []string{"build"}
		}
	}

	m.Config = render(old.Base, cmds)
	if _, err := Parse([]byte(m.Config)); err != nil {
		return nil, fmt.Errorf("the migrated reliab.yaml does not load, a reliab bug: %w\n%s", err, m.Config)
	}
	if old.Debug != nil {
		b, err := yaml.Marshal(old.Debug)
		if err != nil {
			return nil, err
		}
		m.Debug = string(b)
	}
	note("roles, tier budgets, protected paths, relevance rules and corrections have no reliab.yaml form and were dropped")
	note("add a timeout to slow commands, and a targeted line to tools that can run on {files}, {projects} or {tests}")
	sort.Strings(m.Notes)
	return m, nil
}

// convertLine fills v1 placeholders that have no v2 meaning with the value
// they had in that context, and maps the rest.
func convertLine(run, ctx string, old oldProfile, cases, fuzz int, note func(string, ...any), name string) string {
	return oldPlaceholderRe.ReplaceAllStringFunc(run, func(ph string) string {
		switch ph[1 : len(ph)-1] {
		case "impacted":
			if ctx == "change" {
				return "{projects}"
			}
			if old.Impacted != nil && old.Impacted.All != "" {
				return old.Impacted.All
			}
			note("commands.%s: {impacted} had no impacted.all; check the %s line", name, ctx)
			return ""
		case "base":
			if old.Base != "" {
				return old.Base
			}
			return "origin/main"
		case "tests", "mutation_tests":
			return "{tests}"
		case "cases":
			return strconv.Itoa(cases)
		case "fuzz_seconds":
			return strconv.Itoa(fuzz)
		case "seed":
			return "$RELIAB_SEED"
		case "scope":
			note("commands.%s: {scope} is gone; use --target with {files} or {projects}", name)
			return ""
		}
		note("commands.%s: %s has no reliab.yaml form and was removed from the %s line", name, ph, ctx)
		return ""
	})
}

func render(base string, cmds map[string]*Command) string {
	names := make([]string, 0, len(cmds))
	for n := range cmds {
		names = append(names, n)
	}
	order := func(n string) int {
		for i, id := range oldTiers["audit"] {
			if id == n || merged[id] == n {
				return i
			}
		}
		return len(oldTiers["audit"])
	}
	sort.SliceStable(names, func(i, j int) bool {
		if order(names[i]) != order(names[j]) {
			return order(names[i]) < order(names[j])
		}
		return names[i] < names[j]
	})

	var b strings.Builder
	b.WriteString("# Migrated from reliability-profile.yaml by reliab profile migrate.\n")
	b.WriteString("# knowledge/decisions/profile-v2.md explains each key.\n")
	b.WriteString("version: 1\n")
	if base != "" {
		fmt.Fprintf(&b, "base: %s\n", base)
	}
	b.WriteString("\ncommands:\n")
	for i, n := range names {
		c := cmds[n]
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "  %s:\n", n)
		fmt.Fprintf(&b, "    about: %s\n", quote(c.About))
		for _, kv := range [][2]string{{"change", c.Change}, {"pr", c.PR}, {"audit", c.Audit}} {
			if kv[1] != "" {
				fmt.Fprintf(&b, "    %s: %s\n", kv[0], quote(kv[1]))
			}
		}
		if c.Output != nil {
			fmt.Fprintf(&b, "    output: { file: %s, parse: %s }\n", quote(c.Output.File), c.Output.Parse)
		}
		if c.Threshold > 0 {
			fmt.Fprintf(&b, "    threshold: %g\n", c.Threshold)
		}
		if len(c.After) > 0 {
			fmt.Fprintf(&b, "    after: [%s]\n", strings.Join(c.After, ", "))
		}
		if c.Exclusive {
			b.WriteString("    exclusive: true\n")
		}
	}
	return b.String()
}

// quote renders s as a YAML scalar: plain when that reads back the same,
// double-quoted otherwise.
func quote(s string) string {
	var v string
	if err := yaml.Unmarshal([]byte("v: "+s), &struct {
		V *string `yaml:"v"`
	}{&v}); err == nil && v == s && !strings.ContainsAny(s, "{}[]#\"'") {
		return s
	}
	b, _ := yaml.Marshal(s)
	out := strings.TrimSpace(string(b))
	if strings.HasPrefix(out, "|") || strings.HasPrefix(out, ">") || !strings.HasPrefix(out, `"`) && !strings.HasPrefix(out, "'") {
		return strconv.Quote(s)
	}
	return out
}
