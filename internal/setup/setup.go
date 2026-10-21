// Package setup implements reliab profile init: it writes the repository
// side of reliab (profile skeleton, ignore rule, agent settings, AGENTS.md
// rules, version pin) and never any CI configuration
// (knowledge/decisions/ci-agnostic.md). Every step is idempotent.
package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/version"
	"github.com/hbaldwin98/reliab/templates"
)

// Options for Init.
type Options struct {
	Name      string // profile name for a new profile; default: directory name
	Condition string // starting condition for a new profile
}

// Marketplace is where the reliab plugin is installed from.
const (
	MarketplaceName = "reliab"
	MarketplaceRepo = "hbaldwin98/reliab"
	PluginID        = "reliab@reliab"
)

const (
	agentsBegin = "<!-- reliab:begin -->"
	agentsEnd   = "<!-- reliab:end -->"
)

// Init sets up root and returns a line per change it made.
func Init(root string, opt Options) ([]string, *profile.Profile, error) {
	var done []string
	p, created, err := ensureProfile(root, opt)
	if err != nil {
		return nil, nil, err
	}
	if created {
		done = append(done, "wrote "+profile.DefaultPath+" (fill it in during stage S0)")
	} else {
		var msg string
		if p, msg, err = ensureSelfProtected(root, p); err != nil {
			return done, p, err
		}
		if msg != "" {
			done = append(done, msg)
		}
	}
	steps := []func(string, *profile.Profile) (string, error){ensureIgnore, ensureSettings, ensureAgents, ensureVersionPin}
	for _, step := range steps {
		msg, err := step(root, p)
		if err != nil {
			return done, p, err
		}
		if msg != "" {
			done = append(done, msg)
		}
	}
	return done, p, nil
}

func ensureProfile(root string, opt Options) (*profile.Profile, bool, error) {
	path := filepath.Join(root, profile.DefaultPath)
	if _, err := os.Stat(path); err == nil {
		p, err := profile.Load(path)
		return p, false, err
	}
	switch opt.Condition {
	case "greenfield", "maintained", "legacy":
	default:
		return nil, false, fmt.Errorf("--starting-condition is required for a new profile: greenfield, maintained, or legacy")
	}
	name := opt.Name
	if name == "" {
		name = strings.ToLower(regexp.MustCompile(`[^A-Za-z0-9-]+`).ReplaceAllString(filepath.Base(root), "-"))
	}
	body := strings.NewReplacer("%NAME%", name, "%CONDITION%", opt.Condition).Replace(templates.Profile)
	p, err := profile.Parse([]byte(body))
	if err != nil {
		return nil, false, fmt.Errorf("profile template is invalid: %w", err)
	}
	return p, true, os.WriteFile(path, []byte(body), 0o644)
}

func ensureIgnore(root string, _ *profile.Profile) (string, error) {
	path := filepath.Join(root, ".gitignore")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		switch strings.TrimSpace(line) {
		case ".reliab/", ".reliab", "/.reliab/", "/.reliab":
			return "", nil
		}
	}
	if len(raw) > 0 && !bytes.HasSuffix(raw, []byte("\n")) {
		raw = append(raw, '\n')
	}
	raw = append(raw, []byte("\n# reliab reports and tool output (written only by reliab)\n.reliab/\n")...)
	return "added .reliab/ to .gitignore", os.WriteFile(path, raw, 0o644)
}

// ensureSettings merges reliab's entries into .claude/settings.json,
// keeping everything already there.
func ensureSettings(root string, p *profile.Profile) (string, error) {
	path := filepath.Join(root, ".claude", "settings.json")
	s := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("%s does not parse; fix it and re-run: %w", path, err)
		}
	case !os.IsNotExist(err):
		return "", err
	}
	before, _ := json.Marshal(s)

	perms := object(s, "permissions")
	appendUnique(perms, "deny", profile.DenyRules(root, p)...)
	appendUnique(perms, "allow", "Bash(reliab *)")
	object(object(s, "extraKnownMarketplaces"), MarketplaceName)["source"] = map[string]any{
		"source": "github", "repo": MarketplaceRepo,
	}
	object(s, "enabledPlugins")[PluginID] = true
	// Effective only when the user enables the sandbox, which reliab never
	// does for them: it also sandboxes every command and restricts network.
	if dirs := sandboxDirs(p.ProtectedPaths); len(dirs) > 0 {
		appendUnique(object(object(s, "sandbox"), "filesystem"), "denyWrite", dirs...)
	}

	after, _ := json.Marshal(s)
	if bytes.Equal(before, after) {
		return "", nil
	}
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return "updated .claude/settings.json (deny rules, reliab allow rule, marketplace, plugin)", os.WriteFile(path, append(out, '\n'), 0o644)
}

// sandboxDirs converts protected globs of the form "dir/**" into the
// sandbox's project-relative "./dir" form; other globs have no safe
// equivalent and are left to the deny rules and hooks.
func sandboxDirs(globs []string) []string {
	var out []string
	for _, g := range globs {
		dir, ok := strings.CutSuffix(g, "/**")
		if ok && dir != "" && !strings.ContainsAny(dir, "*?[{") {
			out = append(out, "./"+dir)
		}
	}
	return out
}

func object(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	v := map[string]any{}
	m[key] = v
	return v
}

func appendUnique(m map[string]any, key string, values ...string) {
	list, _ := m[key].([]any)
	have := map[string]bool{}
	for _, v := range list {
		if s, ok := v.(string); ok {
			have[s] = true
		}
	}
	for _, v := range values {
		if !have[v] {
			list = append(list, v)
			have[v] = true
		}
	}
	m[key] = list
}

// ensureAgents adds or refreshes the testing-rules section in AGENTS.md,
// between markers, leaving the rest of the file alone.
func ensureAgents(root string, _ *profile.Profile) (string, error) {
	path := filepath.Join(root, "AGENTS.md")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	section := agentsBegin + "\n" + strings.TrimSpace(templates.Agents) + "\n" + agentsEnd + "\n"
	var next string
	if i, j := strings.Index(text, agentsBegin), strings.Index(text, agentsEnd); i >= 0 && j > i {
		next = text[:i] + section + strings.TrimPrefix(text[j+len(agentsEnd):], "\n")
	} else {
		next = text
		if next != "" && !strings.HasSuffix(next, "\n") {
			next += "\n"
		}
		if next != "" {
			next += "\n"
		}
		next += section
	}
	if next == text {
		return "", nil
	}
	return "added the testing rules to AGENTS.md", os.WriteFile(path, []byte(next), 0o644)
}

func ensureVersionPin(root string, _ *profile.Profile) (string, error) {
	path := filepath.Join(root, ".reliab-version")
	if _, err := os.Stat(path); err == nil {
		return "", nil
	}
	if version.IsDev() {
		return "skipped .reliab-version: this is a dev build; pin a released version for CI", nil
	}
	return "wrote .reliab-version (" + version.Version + ")", os.WriteFile(path, []byte(version.Version+"\n"), 0o644)
}
