// Package runner loads reliab.yaml, resolves a target, plans which commands
// run in a context, and runs them (knowledge/decisions/profile-v2.md).
package runner

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/hbaldwin98/reliab/internal/adapters"
)

// DefaultPath is where reliab looks for the v2 profile, relative to the repo root.
const DefaultPath = "reliab.yaml"

// Contexts are the run contexts, in order of depth.
var Contexts = []string{"change", "pr", "audit"}

// Placeholders are the only names a targeted line may use.
var Placeholders = []string{"files", "projects", "tests"}

// Config is reliab.yaml.
type Config struct {
	Version  int                 `yaml:"version"`
	Base     string              `yaml:"base,omitempty"`
	Commands map[string]*Command `yaml:"commands"`
}

// Command is one entry under commands.
type Command struct {
	Name      string   `yaml:"-"`
	About     string   `yaml:"about"`
	Change    string   `yaml:"change,omitempty"`
	PR        string   `yaml:"pr,omitempty"`
	Audit     string   `yaml:"audit,omitempty"`
	Targeted  string   `yaml:"targeted,omitempty"`
	Output    *Output  `yaml:"output,omitempty"`
	After     []string `yaml:"after,omitempty"`
	Exclusive bool     `yaml:"exclusive,omitempty"`
	// Threshold is the lowest mutation score, in percent, that passes. It
	// applies to an output whose parser counts mutants.
	Threshold float64       `yaml:"threshold,omitempty"`
	Timeout   time.Duration `yaml:"-"`
	RawTO     string        `yaml:"timeout,omitempty"`
}

// Output is the file a command writes and the adapter that parses it.
type Output struct {
	File  string `yaml:"file"`
	Parse string `yaml:"parse"`
}

// Line returns the command's line for a context, "" when it does not run there.
func (c *Command) Line(context string) string {
	switch context {
	case "change":
		return c.Change
	case "pr":
		return c.PR
	case "audit":
		return c.Audit
	}
	return ""
}

var placeholderRe = regexp.MustCompile(`\{([^{}\s]+)\}`)

// Load reads and validates a reliab.yaml.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes and validates reliab.yaml content. Unknown keys are errors,
// so a typo cannot silently drop a command.
func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("reliab.yaml: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("reliab.yaml: %w", err)
	}
	return &cfg, nil
}

func (cfg *Config) validate() error {
	if cfg.Version != 1 {
		return fmt.Errorf("version must be 1, got %d", cfg.Version)
	}
	if len(cfg.Commands) == 0 {
		return errors.New("commands: at least one command is required")
	}
	var errs []error
	for _, name := range cfg.Names() {
		c := cfg.Commands[name]
		if c == nil {
			errs = append(errs, fmt.Errorf("commands.%s: empty", name))
			continue
		}
		c.Name = name
		if strings.TrimSpace(c.About) == "" {
			errs = append(errs, fmt.Errorf("commands.%s: about is required: say what it checks and what a failure means", name))
		}
		if c.Change == "" && c.PR == "" && c.Audit == "" && c.Targeted == "" {
			errs = append(errs, fmt.Errorf("commands.%s: needs a line for at least one of change, pr, audit, targeted", name))
		}
		for _, key := range append(slices.Clone(Contexts), "targeted") {
			line := c.Targeted
			if key != "targeted" {
				line = c.Line(key)
			}
			for _, m := range placeholderRe.FindAllStringSubmatch(line, -1) {
				if !contains(Placeholders, m[1]) {
					errs = append(errs, fmt.Errorf("commands.%s.%s: unknown placeholder {%s}; use {files}, {projects} or {tests}", name, key, m[1]))
				}
			}
		}
		if c.Output != nil && (c.Output.File == "" || c.Output.Parse == "") {
			errs = append(errs, fmt.Errorf("commands.%s.output: needs file and parse", name))
		} else if c.Output != nil && !adapters.Supported(c.Output.Parse) {
			errs = append(errs, fmt.Errorf("commands.%s.output.parse: unknown parser %q; use one of %s", name, c.Output.Parse, strings.Join(adapters.Formats(), ", ")))
		}
		if c.Threshold < 0 || c.Threshold > 100 {
			errs = append(errs, fmt.Errorf("commands.%s.threshold: %v is not a percent from 0 to 100", name, c.Threshold))
		} else if c.Threshold > 0 && c.Output == nil {
			errs = append(errs, fmt.Errorf("commands.%s.threshold: needs an output whose parser counts mutants", name))
		}
		for _, a := range c.After {
			if a == name {
				errs = append(errs, fmt.Errorf("commands.%s.after: names itself", name))
			} else if _, ok := cfg.Commands[a]; !ok {
				errs = append(errs, fmt.Errorf("commands.%s.after: no command %q", name, a))
			}
		}
		if c.RawTO != "" {
			d, err := time.ParseDuration(c.RawTO)
			if err != nil || d <= 0 {
				errs = append(errs, fmt.Errorf("commands.%s.timeout: %q is not a duration like 90s or 30m", name, c.RawTO))
			}
			c.Timeout = d
		}
	}
	if len(errs) == 0 {
		if cyc := cfg.cycle(); cyc != nil {
			errs = append(errs, fmt.Errorf("after: cycle %s", strings.Join(cyc, " -> ")))
		}
	}
	return errors.Join(errs...)
}

// Names returns command names sorted.
func (cfg *Config) Names() []string {
	names := make([]string, 0, len(cfg.Commands))
	for n := range cfg.Commands {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// cycle returns one after-cycle, or nil.
func (cfg *Config) cycle() []string {
	const (
		unseen = iota
		active
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(string) []string
	visit = func(n string) []string {
		state[n] = active
		stack = append(stack, n)
		for _, a := range cfg.Commands[n].After {
			switch state[a] {
			case active:
				i := indexOf(stack, a)
				return append(slices.Clone(stack[i:]), a)
			case unseen:
				if c := visit(a); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = done
		return nil
	}
	for _, n := range cfg.Names() {
		if state[n] == unseen {
			if c := visit(n); c != nil {
				return c
			}
		}
	}
	return nil
}

func contains(xs []string, s string) bool { return indexOf(xs, s) >= 0 }

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}
