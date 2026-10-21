package guard

import (
	"path"
	"strings"
)

// The person-only regexes (ackCmd and the rest) match reliab's words as
// typed, so quoting, an escape, a global flag, or a variable got past them:
// reliab "findings" ack, reli\ab findings ack, reliab --repo . findings ack,
// $r findings ack. personOnly reads each simple command's words as the
// shell does and the command path as cobra resolves it.

// personCmds are the reliab command paths only a person runs, and report
// verdict, which only the reviewer runs.
var personCmds = [][]string{{"findings", "ack"}, {"approve"}, {"baseline", "write"}, {"report", "verdict"}}

// personOnly returns the person-only command path, such as "findings ack",
// that a simple command of code may run, or "". pwsh says the line is
// PowerShell.
func personOnly(code string, pwsh bool) string {
	for _, s := range commandSep.Split(code, -1) {
		ws := words(s, pwsh)
		for i, w := range ws {
			if !mayBeReliab(w, pwsh) {
				continue
			}
			if c := commandPath(ws[i+1:]); c != "" {
				return c
			}
		}
	}
	return ""
}

// mayBeReliab reports whether a word may name the reliab binary: by its base
// name, with or without .exe, .cmd, .bat, or .ps1, in any case; or a
// variable, which may hold it. In PowerShell a backtick escapes the next
// character.
func mayBeReliab(w string, pwsh bool) bool {
	if pwsh {
		w = strings.ReplaceAll(w, "`", "")
	}
	if strings.HasPrefix(w, "$") {
		return true
	}
	base := path.Base(strings.ReplaceAll(strings.ToLower(w), `\`, "/"))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		base = strings.TrimSuffix(base, ext)
	}
	return base == "reliab"
}

// commandPath returns the person-only command path args may select, or "".
// Cobra finds a command path among the words that are not flags, and takes
// the word after a flag with no = as that flag's value; the hook does not
// know which flags take one, so such a word may be either.
func commandPath(args []string) string {
	var pos []string
	var value []bool // the positional word may be a flag's value
	for k, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		pos = append(pos, strings.ToLower(a))
		value = append(value, k > 0 && strings.HasPrefix(args[k-1], "-") && !strings.Contains(args[k-1], "="))
	}
	for _, c := range personCmds {
		if matchPath(pos, value, c) {
			return strings.Join(c, " ")
		}
	}
	return ""
}

// matchPath reports whether the positional words hold cmd's words in order,
// with only possible flag values before and between them.
func matchPath(pos []string, value []bool, cmd []string) bool {
	if len(cmd) == 0 {
		return true
	}
	for j := range pos {
		if pos[j] == cmd[0] && matchPath(pos[j+1:], value[j+1:], cmd[1:]) {
			return true
		}
		if !value[j] {
			return false
		}
	}
	return false
}
