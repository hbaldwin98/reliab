package dap

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"

	"go.yaml.in/yaml/v3"
)

// ConfigPath is the debug settings file, relative to the repo root
// (knowledge/decisions/profile-v2.md: debug moves out of the profile).
const ConfigPath = "reliab-debug.yaml"

// Config is reliab-debug.yaml. Placeholders in every string value: {port},
// {cwd}, {root}, {pid}, and names given with --set.
type Config struct {
	Adapter AdapterSpec `yaml:"adapter"`
	// Launch and Attach are the DAP request arguments, passed verbatim
	// after expansion.
	Launch map[string]any `yaml:"launch,omitempty"`
	Attach map[string]any `yaml:"attach,omitempty"`
	// SourceRoot is where adapter paths are made repo-relative from;
	// default the repository root.
	SourceRoot string `yaml:"source_root,omitempty"`
	Notes      string `yaml:"notes,omitempty"`
}

// AdapterSpec is how to start the adapter.
type AdapterSpec struct {
	// Run is the adapter's command line; its first word is looked up on PATH.
	Run string `yaml:"run"`
	// Transport is stdio (DAP on stdin and stdout) or tcp (the adapter
	// listens on 127.0.0.1:{port}).
	Transport string `yaml:"transport"`
	// Ready, tcp only, is a regular expression on the adapter's output that
	// means it listens. Without it reliab retries for 5 seconds.
	Ready string `yaml:"ready,omitempty"`
}

// LoadConfig reads and checks a reliab-debug.yaml.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// ParseConfig decodes reliab-debug.yaml content; unknown keys are errors.
func ParseConfig(raw []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	var errs []error
	if c.Adapter.Run == "" {
		errs = append(errs, errors.New("adapter.run is required"))
	}
	if c.Adapter.Transport != TransportStdio && c.Adapter.Transport != TransportTCP {
		errs = append(errs, fmt.Errorf("adapter.transport must be stdio or tcp, got %q", c.Adapter.Transport))
	}
	if c.Adapter.Ready != "" {
		if _, err := regexp.Compile(c.Adapter.Ready); err != nil {
			errs = append(errs, fmt.Errorf("adapter.ready: %w", err))
		}
	}
	return &c, errors.Join(errs...)
}
