package effects

import (
	"path"
	"strings"
)

// A reliab invocation is found wherever a shell may run one: a word whose
// binary name is reliab (also inside a substitution, as in $(which reliab)
// or (Get-Command reliab)) starts one wherever it stands, and so does a
// variable where a command goes (first in Bash, or after a wrapper such as
// env, sudo, xargs, or PowerShell's & call operator). Its command path is
// found as cobra finds it (commandPath). It does not see through a
// function or alias, a script, or another language's process call; CI and
// review are the backstop.

// callWrappers run the command that follows them, so a variable after one
// is in command position.
var callWrappers = map[string]bool{"&": true, ".": true, "env": true, "sudo": true, "nohup": true, "xargs": true, "time": true, "command": true, "exec": true, "call": true}

// call is one reliab invocation: the words after the binary.
type call struct {
	args []string
	pwsh bool
}

// calls returns the reliab invocations among a simple command's words
// before upTo; the words from upTo on are data.
func calls(ws []word, pwsh bool, upTo int) []call {
	texts := make([]string, len(ws))
	for i, w := range ws {
		texts[i] = w.text
	}
	v := verb(texts)
	var out []call
	for i := 0; i < len(ws) && i < upTo; i++ {
		n, variable := binaryName(texts[i])
		if n != "reliab" && namesReliab(ws[i].subs) {
			n = "reliab"
		}
		// A variable not first comes after the command's name, so i > 0.
		// PowerShell reads a variable first in a statement as a value,
		// never a command.
		runs := n == "reliab" || variable && (i == v && !pwsh || i > 0 && callWrappers[strings.ToLower(texts[i-1])])
		if runs {
			out = append(out, call{texts[i+1:], pwsh})
		}
	}
	return out
}

// namesReliab reports whether code substituted into a word names the
// reliab binary, as $(which reliab) and (Get-Command reliab) do.
func namesReliab(subs []*line) bool {
	for _, s := range subs {
		for _, c := range s.cmds {
			for _, w := range c.words {
				if n, _ := binaryName(w.text); n == "reliab" || namesReliab(w.subs) {
					return true
				}
			}
			if namesReliab(c.groups) {
				return true
			}
		}
	}
	return false
}

// binaryName returns the name a word runs: its base name, lower-cased, with
// no .exe, .cmd, .bat, or .ps1, no @version (go run pkg@v1), and the
// package directory's name for go run's main.go. A ( or $( and ) around
// it (a subshell, $(which reliab), (Get-Command reliab)) are dropped.
// variable says the base name is a variable, which may hold any binary.
func binaryName(w string) (string, bool) {
	w = strings.TrimPrefix(w, "$(")
	w = strings.TrimLeft(w, "(`")
	w, _, _ = strings.Cut(w, ")")
	w, _, _ = strings.Cut(w, "`")
	p := strings.ReplaceAll(strings.ToLower(w), `\`, "/")
	base := path.Base(p)
	if base == "main.go" {
		base = path.Base(path.Dir(p))
	}
	base, _, _ = strings.Cut(base, "@")
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		base = strings.TrimSuffix(base, ext)
	}
	return base, strings.HasPrefix(base, "$")
}

// commandPath reports whether args may select the command path cmd. Cobra
// finds a command path among the words that are not flags, and takes the
// word after a flag with no = as that flag's value; the hook does not know
// which flags take one, so such a word may be either. A redirect counts as
// a flag. A variable may hold any one word and a PowerShell splat (@args)
// any words; a PowerShell comma list (Start-Process -ArgumentList
// findings,ack) and a quoted argument holding spaces count word by word.
func commandPath(args []string, pwsh bool, cmd []string) bool {
	var split []string
	for _, a := range args {
		sep := " \t"
		if pwsh {
			sep += ","
		}
		split = append(split, strings.FieldsFunc(a, func(r rune) bool { return strings.ContainsRune(sep, r) })...)
	}
	var pos []string
	var value []bool // the positional word may be a flag's value
	for k, a := range split {
		if isFlag(a) {
			continue
		}
		pos = append(pos, a)
		value = append(value, k > 0 && isFlag(split[k-1]) && !strings.Contains(split[k-1], "="))
	}
	return matchPath(pos, value, cmd, pwsh)
}

// isFlag reports whether a word is an option or a redirect (>x, 2>, <in).
func isFlag(a string) bool {
	r := strings.TrimLeft(a, "0123456789&")
	return strings.HasPrefix(a, "-") || strings.HasPrefix(r, "<") || strings.HasPrefix(r, ">")
}

// matchPath reports whether the positional words hold cmd's words in order,
// with only possible flag values before and between them.
func matchPath(pos []string, value []bool, cmd []string, pwsh bool) bool {
	if len(cmd) == 0 {
		return true
	}
	for j := range pos {
		if pwsh && strings.HasPrefix(pos[j], "@") {
			return true
		}
		if (pos[j] == cmd[0] || strings.HasPrefix(pos[j], "$")) && matchPath(pos[j+1:], value[j+1:], cmd[1:], pwsh) {
			return true
		}
		if !value[j] {
			return false
		}
	}
	return false
}
