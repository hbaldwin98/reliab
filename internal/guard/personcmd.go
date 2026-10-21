package guard

import (
	"path"
	"strings"
)

// The person-only regexes (ackCmd and the rest) match reliab's words as
// typed, so quoting, an escape, a global flag, or a variable got past them:
// reliab "findings" ack, reli\ab findings ack, reliab --repo . findings ack,
// $r findings ack. personOnly reads each simple command's words as the
// shell does and the command path as cobra resolves it. It does not see
// through a function or alias, a script, or another language's process
// call; CI and review are the backstop.

// personCmds are the reliab command paths only a person runs, and report
// verdict, which only the reviewer runs.
var personCmds = [][]string{{"findings", "ack"}, {"approve"}, {"baseline", "write"}, {"report", "verdict"}}

// wrappers run the command that follows them, so a variable after one is in
// command position.
var wrappers = map[string]bool{"&": true, ".": true, "env": true, "sudo": true, "nohup": true, "xargs": true, "time": true, "command": true, "exec": true, "call": true}

// personOnly returns the person-only command path, such as "findings ack",
// that a simple command of code may run, or "". pwsh says the line is
// PowerShell.
func personOnly(code string, pwsh bool) string {
	// A line continuation joins two lines into one command (GNU Bash manual
	// §3.1.2.1; a PowerShell backtick before a newline).
	code = strings.NewReplacer("\\\r\n", " ", "\\\n", " ", "`\r\n", " ", "`\n", " ").Replace(code)
	if pwsh {
		// PowerShell reads curly quotes as quotes and a no-break space as
		// a space (about_Quoting_Rules).
		code = strings.NewReplacer("“", `"`, "”", `"`, "‘", "'", "’", "'", " ", " ").Replace(code)
	}
	for _, u := range units(code, commandSep.FindAllStringIndex(code, -1)) {
		ws := words(u.text, pwsh)
		if pwsh {
			for i, w := range ws {
				ws[i] = strings.ReplaceAll(w, "`", "")
			}
		}
		v := verb(ws)
		for i, w := range ws {
			// Code nested in a word, as in bash -c '...', pwsh -c, iex, or
			// cmd /c, is read as a command line of its own. Such a word
			// lost its quotes, so it is shorter than code: this ends.
			if strings.ContainsAny(w, " \t") {
				if c := personOnly(w, pwsh); c != "" {
					return c
				}
			}
			name, variable := binaryName(w)
			// A variable not first comes after the command's name, so i > 0.
			runs := name == "reliab" || variable && (i == v || wrappers[strings.ToLower(ws[i-1])])
			if !runs {
				continue
			}
			if c := commandPath(ws[i+1:], pwsh); c != "" {
				return c
			}
		}
	}
	return ""
}

// binaryName returns the name a word runs: its base name, lower-cased, with
// no .exe, .cmd, .bat, or .ps1, no @version (go run pkg@v1), and the
// package directory's name for go run's main.go. A ( or $( and ) around
// it (a subshell, $(which reliab), (Get-Command reliab)) are dropped.
// variable says the base name is a variable, which may hold any binary.
func binaryName(w string) (string, bool) {
	w = strings.TrimPrefix(w, "$(")
	w = strings.TrimLeft(w, "(`")
	if k := strings.IndexAny(w, ")`"); k >= 0 {
		w = w[:k]
	}
	p := strings.ReplaceAll(strings.ToLower(w), `\`, "/")
	base := path.Base(p)
	if base == "main.go" {
		base = path.Base(path.Dir(p))
	}
	if k := strings.IndexByte(base, '@'); k > 0 {
		base = base[:k]
	}
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		base = strings.TrimSuffix(base, ext)
	}
	return base, strings.HasPrefix(base, "$")
}

// commandPath returns the person-only command path args may select, or "".
// Cobra finds a command path among the words that are not flags, and takes
// the word after a flag with no = as that flag's value; the hook does not
// know which flags take one, so such a word may be either. A redirect
// counts as a flag. A variable may hold any one word and a PowerShell splat
// (@args) any words; a PowerShell comma list (Start-Process -ArgumentList
// findings,ack) and a quoted argument holding spaces count word by word.
func commandPath(args []string, pwsh bool) string {
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
	for _, c := range personCmds {
		if matchPath(pos, value, c, pwsh) {
			return strings.Join(c, " ")
		}
	}
	return ""
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
