package dap

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// placeholder matches {name} in a profile string.
var placeholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

var setName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Builtins are the placeholders reliab fills itself; --set cannot name them.
var Builtins = []string{"port", "cwd", "root", "pid"}

// numeric placeholders become a JSON number when they are a string's whole
// value, since adapters take a port or a process id as a number.
var numeric = map[string]bool{"port": true, "pid": true}

// ParseSets turns --set name=value flags into placeholder values.
func ParseSets(sets []string) (map[string]string, error) {
	out := map[string]string{}
	for _, s := range sets {
		name, value, ok := strings.Cut(s, "=")
		if !ok || !setName.MatchString(name) {
			return nil, fmt.Errorf("--set %q: want name=value, with a name of letters, digits, and _", s)
		}
		for _, b := range Builtins {
			if name == b {
				return nil, fmt.Errorf("--set %s: {%s} is reliab's own placeholder", name, name)
			}
		}
		out[name] = value
	}
	return out, nil
}

// ExpandString fills the placeholders in s. Unknown placeholders are
// collected in missing.
func ExpandString(s string, vars map[string]string, missing map[string]bool) string {
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		if v, ok := vars[name]; ok {
			return v
		}
		missing[name] = true
		return m
	})
}

// Expand fills placeholders in every string value of v, at any depth, and
// returns a copy. Keys are left alone. A string that is exactly {port} or
// {pid} becomes a number. Any placeholder without a value is an error
// naming where it is.
func Expand(where string, v any, vars map[string]string) (any, error) {
	missing := map[string]bool{}
	var at []string
	out := expandValue(where, v, vars, missing, &at)
	if len(missing) > 0 {
		return nil, missingError(missing, at)
	}
	return out, nil
}

func expandValue(path string, v any, vars map[string]string, missing map[string]bool, at *[]string) any {
	switch x := v.(type) {
	case string:
		if m := placeholder.FindStringSubmatch(x); m != nil && m[0] == x && numeric[m[1]] {
			if val, ok := vars[m[1]]; ok {
				if n, err := strconv.Atoi(val); err == nil {
					return n
				}
			}
		}
		if hasUnknown(x, vars) {
			*at = append(*at, path)
		}
		return ExpandString(x, vars, missing)
	case map[string]any:
		out := make(map[string]any, len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out[k] = expandValue(path+"."+k, x[k], vars, missing, at)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = expandValue(fmt.Sprintf("%s[%d]", path, i), e, vars, missing, at)
		}
		return out
	default:
		return v
	}
}

func hasUnknown(s string, vars map[string]string) bool {
	for _, m := range placeholder.FindAllStringSubmatch(s, -1) {
		if _, ok := vars[m[1]]; !ok {
			return true
		}
	}
	return false
}

func missingError(missing map[string]bool, at []string) error {
	names := make([]string, 0, len(missing))
	for n := range missing {
		names = append(names, "{"+n+"}")
	}
	sort.Strings(names)
	hint := "give each with --set name=value"
	for n := range missing {
		switch n {
		case "pid":
			hint = "{pid} comes from --pid; " + hint
		case "port":
			hint = "{port} exists only with transport tcp; " + hint
		}
	}
	return fmt.Errorf("unexpanded placeholder %s in %s; %s", strings.Join(names, ", "), strings.Join(at, ", "), hint)
}
