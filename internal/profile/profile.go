// Package profile loads and validates reliability-profile.yaml.
package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/hbaldwin98/reliab/schemas"
)

// DefaultPath is where reliab looks for the profile, relative to the repo root.
const DefaultPath = "reliability-profile.yaml"

// Profile mirrors schemas/profile.schema.json.
type Profile struct {
	SchemaVersion          string             `json:"schemaVersion"`
	Name                   string             `json:"name"`
	StartingCondition      string             `json:"starting_condition"`
	VerifiedOn             string             `json:"verified_on,omitempty"`
	Runtime                *Runtime           `json:"runtime,omitempty"`
	Paths                  Paths              `json:"paths"`
	ProtectedPaths         []string           `json:"protected_paths,omitempty"`
	ProtectedCommands      []string           `json:"protected_commands,omitempty"`
	ProtectedEdits         []string           `json:"protected_edits,omitempty"`
	Base                   string             `json:"base,omitempty"`
	FreshnessExclude       []string           `json:"freshness_exclude,omitempty"`
	MutationBaseline       string             `json:"mutation_baseline,omitempty"`
	Roles                  map[string]Role    `json:"roles"`
	Commands               map[string]Command `json:"commands"`
	Impacted               *Impacted          `json:"impacted,omitempty"`
	Tiers                  map[string]Tier    `json:"tiers,omitempty"`
	HostAdapter            *HostAdapter       `json:"host_adapter,omitempty"`
	Debug                  *Debug             `json:"debug,omitempty"`
	RequireReviewerVerdict bool               `json:"require_reviewer_verdict,omitempty"`
	Corrections            []Correction       `json:"corrections,omitempty"`
}

type Runtime struct {
	Current   string `json:"current"`
	Target    string `json:"target,omitempty"`
	SharedLib string `json:"shared_lib,omitempty"`
}

type Paths struct {
	Product []string `json:"product"`
	Tests   []string `json:"tests"`
	Ignore  []string `json:"ignore,omitempty"`
}

// Role is a filling (Tool set), not applicable (Applicable false with a
// Reason), or needed but missing (Missing with a Reason): the codebase needs
// the role and has no tool for it yet.
type Role struct {
	Tool       string `json:"tool,omitempty"`
	Version    string `json:"version,omitempty"`
	Verified   string `json:"verified,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Applicable *bool  `json:"applicable,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Missing    bool   `json:"missing,omitempty"`
}

// Command is what verify runs for one command id. A bare string in the
// profile is shorthand for Run with Format "none".
type Command struct {
	Run    string `json:"run"`
	Format string `json:"format,omitempty"`
	// Output is a path or a glob; every match is read.
	Output string `json:"output,omitempty"`
	// ChangedOnly keeps a mutation or coverage report to the lines changed
	// since the base.
	ChangedOnly bool `json:"changed_only,omitempty"`
	// Threshold is a mutation command's minimum score in percent
	// (knowledge/decisions/mutation-threshold.md).
	Threshold *float64 `json:"threshold,omitempty"`
	// RelevantWhen are globs of the files that make the command relevant:
	// in the inner and prepr tiers it is skipped when the change touches
	// none of them (knowledge/decisions/relevance-skips.md).
	RelevantWhen []string `json:"relevant_when,omitempty"`
	// Needs are the per-run inputs the command cannot run without: tests
	// (verify --tests) or mutation_tests (verify --mutation-tests). Without
	// them a relevant command leaves the run incomplete
	// (knowledge/decisions/named-tests.md).
	Needs []string `json:"needs,omitempty"`
}

// Per-run inputs a command can need.
const (
	NeedTests         = "tests"
	NeedMutationTests = "mutation_tests"
)

func (c *Command) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*c = Command{Run: s, Format: "none"}
		return nil
	}
	type plain Command
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if p.Format == "" {
		p.Format = "none"
	}
	*c = Command(p)
	return nil
}

type Impacted struct {
	All string        `json:"all"`
	Map []ImpactedMap `json:"map,omitempty"`
	// Separator joins the args of several matched map entries; empty means
	// a space.
	Separator string `json:"separator,omitempty"`
}

type ImpactedMap struct {
	Glob string `json:"glob"`
	Args string `json:"args"`
}

type Tier struct {
	BudgetSeconds *int     `json:"budget_seconds,omitempty"`
	PropertyCases *int     `json:"property_cases,omitempty"`
	FuzzSeconds   *int     `json:"fuzz_seconds,omitempty"`
	Run           []string `json:"run,omitempty"`
}

type HostAdapter struct {
	Class string `json:"class,omitempty"`
	File  string `json:"file"`
}

// Debug names the language's debug adapter and the arguments of its launch
// and attach requests (knowledge/decisions/debug-adapter-seam.md). reliab
// never interprets a launch or attach key.
type Debug struct {
	Adapter    DebugAdapter   `json:"adapter"`
	Launch     map[string]any `json:"launch,omitempty"`
	Attach     map[string]any `json:"attach,omitempty"`
	SourceRoot string         `json:"source_root,omitempty"`
	Notes      string         `json:"notes,omitempty"`
}

// DebugAdapter is how to start the adapter.
type DebugAdapter struct {
	Run       string `json:"run"`
	Transport string `json:"transport"`
	Ready     string `json:"ready,omitempty"`
}

type Correction struct {
	Tool     string `json:"tool"`
	Claimed  string `json:"claimed"`
	Found    string `json:"found"`
	Decision string `json:"decision"`
	Source   string `json:"source,omitempty"`
	Date     string `json:"date"`
}

// Load reads, validates, and decodes the profile at path.
func Load(path string) (*Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// Parse validates YAML profile bytes against the schema and decodes them.
func Parse(raw []byte) (*Profile, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("empty profile")
	}
	val, err := toJSONValue(root.Content[0])
	if err != nil {
		return nil, err
	}
	js, err := json.Marshal(val)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(js))
	if err != nil {
		return nil, err
	}
	if err := schemas.Validate(schemas.Profile, doc); err != nil {
		return nil, err
	}
	var p Profile
	dec := json.NewDecoder(bytes.NewReader(js))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	for i, pat := range p.ProtectedCommands {
		if _, err := regexp.Compile(pat); err != nil {
			return nil, fmt.Errorf("protected_commands[%d]: %w", i, err)
		}
	}
	for id, c := range p.Commands {
		if c.Output != "" && !doublestar.ValidatePattern(c.Output) {
			return nil, fmt.Errorf("commands.%s.output: %q is not a valid glob", id, c.Output)
		}
		for _, g := range c.RelevantWhen {
			if !doublestar.ValidatePattern(g) {
				return nil, fmt.Errorf("commands.%s.relevant_when: %q is not a valid glob", id, g)
			}
		}
		if c.Threshold != nil && !IsMutation(id) {
			return nil, fmt.Errorf("commands.%s.threshold: only a mutation command has a score", id)
		}
	}
	// The baseline is an oracle: a person approves every change to it (R2).
	if b := p.MutationBaseline; b != "" && !matchAny(p.ProtectedPaths, b) {
		return nil, fmt.Errorf("mutation_baseline %s must match protected_paths: only a person changes a baseline (R2)", b)
	}
	for i, pat := range p.ProtectedEdits {
		if _, err := regexp.Compile(pat); err != nil {
			return nil, fmt.Errorf("protected_edits[%d]: %w", i, err)
		}
	}
	if err := checkDebug(p.Debug); err != nil {
		return nil, err
	}
	return &p, nil
}

// checkDebug holds the debug section to what its transport needs.
func checkDebug(d *Debug) error {
	if d == nil {
		return nil
	}
	a := d.Adapter
	if a.Ready != "" {
		if a.Transport != "tcp" {
			return fmt.Errorf("debug.adapter.ready: only a tcp adapter is waited for")
		}
		if _, err := regexp.Compile(a.Ready); err != nil {
			return fmt.Errorf("debug.adapter.ready: %w", err)
		}
	}
	if a.Transport == "tcp" && !strings.Contains(a.Run, "{port}") {
		return fmt.Errorf("debug.adapter.run must name {port} with transport tcp: reliab picks the port")
	}
	return nil
}

func matchAny(globs []string, path string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, path); ok {
			return true
		}
	}
	return false
}

// TreeExclude is what a report's tree hash leaves out: reliab's own output,
// which never makes a report stale, and freshness_exclude
// (knowledge/decisions/report-freshness.md).
func (p *Profile) TreeExclude() []string {
	return append([]string{".reliab/**"}, p.FreshnessExclude...)
}

// CommandGuards compiles protected_commands. Parse has already checked that
// every pattern compiles.
func (p *Profile) CommandGuards() []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(p.ProtectedCommands))
	for _, pat := range p.ProtectedCommands {
		out = append(out, regexp.MustCompile(pat))
	}
	return out
}

// EditGuards compiles protected_edits. Parse has already checked that every
// pattern compiles.
func (p *Profile) EditGuards() []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(p.ProtectedEdits))
	for _, pat := range p.ProtectedEdits {
		out = append(out, regexp.MustCompile(pat))
	}
	return out
}

// toJSONValue converts a YAML node to plain JSON-compatible values. Unlike
// decoding into any, it keeps timestamps such as 2026-10-17 as strings, and
// it rejects duplicate keys, merge keys, and non-scalar keys.
func toJSONValue(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		return toJSONValue(n.Content[0])
	case yaml.AliasNode:
		return toJSONValue(n.Alias)
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Kind != yaml.ScalarNode || k.Tag == "!!merge" {
				return nil, fmt.Errorf("line %d: keys must be plain strings", k.Line)
			}
			if _, dup := m[k.Value]; dup {
				return nil, fmt.Errorf("line %d: duplicate key %q", k.Line, k.Value)
			}
			val, err := toJSONValue(v)
			if err != nil {
				return nil, err
			}
			m[k.Value] = val
		}
		return m, nil
	case yaml.SequenceNode:
		s := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			val, err := toJSONValue(c)
			if err != nil {
				return nil, err
			}
			s = append(s, val)
		}
		return s, nil
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!null":
			return nil, nil
		case "!!bool":
			var b bool
			err := n.Decode(&b)
			return b, err
		case "!!int":
			i, err := strconv.ParseInt(n.Value, 0, 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", n.Line, err)
			}
			return i, nil
		case "!!float":
			var f float64
			err := n.Decode(&f)
			return f, err
		default:
			return n.Value, nil
		}
	}
	return nil, fmt.Errorf("line %d: unsupported YAML node", n.Line)
}

// IsMutation reports whether command id names a mutation command: by the
// profile's naming convention, its id starts with mutation_
// (schemas/profile.schema.json, knowledge/decisions/schema-v0-shape.md).
func IsMutation(id string) bool { return strings.HasPrefix(id, "mutation_") }

// Protects reports whether path matches one of p's protected_paths.
func Protects(p *Profile, path string) bool { return matchAny(p.ProtectedPaths, path) }
