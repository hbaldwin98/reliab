package guard

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

// A relative path in a command line counts where its simple command runs,
// so a write in a scratch repository the line cds into is not a write here.
// For moving a write out, the rule is fail closed: a simple command is
// judged from the session's directory unless the line surely runs it
// elsewhere. Surely means a cd the
// hook can follow (a literal directory that exists and can be entered),
// joined by && to the commands it moves, in a chain nothing on the line can
// reorder or redefine. Any doubt (a variable, cd -, popd, ||, a pipe, a
// loop, a subshell or brace group, a quote hiding a separator, an alias,
// eval, a script that may change location) leaves the session's directory.
//
// That rule only keeps a write from moving out of the repository. A cd also
// stays in effect past ;, || and a newline, and one the hook cannot follow
// may lead anywhere, so cd internal; rm ../x still writes ../x from
// internal. Each part also carries every directory it may run in, and a
// write is judged from those too.

// part is one simple command and the directories it may run in: the
// session's, or after a cd, the directory as the shell names it and as the
// kernel resolves it through symlinks. A path is judged against each, since
// PowerShell resolves .. on the first and every native program on the
// second.
type part struct {
	text string
	cwds []string
	// may holds every directory the part may run in, whatever the
	// separators; anywhere among them means a directory the hook cannot
	// know.
	may []string
}

// anywhere stands, in a part's may, for a directory the hook cannot know: a
// cd to a variable or one that may fail, popd, a script that may cd.
const anywhere = "\x00anywhere"

var (
	// cdVerbs move the commands after them to their one argument.
	cdVerbs = map[string]bool{"cd": true, "chdir": true, "pushd": true, "set-location": true, "sl": true, "push-location": true}
	// cdFlags are the cd options that leave the argument a plain directory
	// resolved logically. Any other (cd -P, pushd -n, -StackName) is doubt.
	cdFlags = map[string]bool{"-l": true, "-path": true, "-literalpath": true, "-passthru": true}
	// keywords may precede a simple command without changing where it runs.
	keywords = map[string]bool{"time": true, "if": true, "then": true, "else": true, "elif": true}
	// untracked verbs make the whole line doubtful: they repeat commands,
	// run text as code, or redefine what cd means.
	untracked = map[string]bool{
		"for": true, "while": true, "until": true, "select": true, "do": true, "done": true,
		"source": true, ".": true, "eval": true, "exec": true, "builtin": true, "command": true,
		"enable": true, "shopt": true, "trap": true, "declare": true, "typeset": true,
		"sal": true, "nal": true, "import-module": true, "ipmo": true,
		"invoke-expression": true, "iex": true, "invoke-command": true, "icm": true,
	}
	// lineDoubt is text that makes a whole line doubtful: substitutions,
	// subshells, brace groups, heredocs, ANSI-C quotes, an escaped quote
	// (Bash and PowerShell disagree on it), PowerShell's stop-parsing token,
	// aliases, function definitions, and CDPATH, which redirects a cd.
	lineDoubt = []string{"`", "(", ")", "{", "}", "<<", "$'", `\'`, `\"`, "--%", "alias", "function", "cdpath"}
	// dirFlags name a directory a program runs in, such as env -C or
	// make --directory.
	dirFlags = map[string]bool{"-C": true, "--directory": true, "--chdir": true, "--work-tree": true}
	assign   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	// cdWord finds a command that changes directory anywhere in a line the
	// hook cannot track: after a separator, ( or $( (a PowerShell group
	// keeps the location), { or &{, a backtick, or an assignment's =.
	cdWord = regexp.MustCompile("(?i)(^|[\\s;&|({`=])(cd|chdir|pushd|popd|sl|set-location|push-location|pop-location)($|[\\s;&|)}`.\\\\/'\"])")
)

// parts splits cmd at ;, &, |, and newlines, as commandSep does, and gives
// each simple command the directories it may run in, starting from the
// session's. pwsh says the line is PowerShell, where a backslash separates
// paths instead of escaping.
func parts(session, cmd string, pwsh bool) []part {
	seps := commandSep.FindAllStringIndex(cmd, -1)
	var out []part
	start := 0
	for k := 0; k <= len(seps); k++ {
		end := len(cmd)
		if k < len(seps) {
			end = seps[k][0]
		}
		out = append(out, part{text: cmd[start:end], cwds: []string{session}, may: []string{session}})
		if k < len(seps) {
			start = seps[k][1]
		}
	}
	if !trackable(cmd) {
		return withDirFlags(unfollowed(out, session, pwsh), session, pwsh)
	}
	us := units(cmd, seps)
	for _, u := range us {
		ws := words(u.text, pwsh)
		if v := verb(ws); v >= 0 && (untracked[strings.ToLower(ws[v])] || pwsh && isScript(ws[v])) {
			return withDirFlags(unfollowed(out, session, pwsh), session, pwsh)
		}
	}
	mayRun(out, us, session, pwsh)
	session1 := []string{session}
	logical, cwds := session, session1
	moved, frozen, prevAnd := false, false, true
	reset := func() { logical, cwds, moved = session, session1, false }
	for _, u := range us {
		for k := u.first; k <= u.last; k++ {
			out[k].cwds = cwds
		}
		kind := sepKind(u.sep)
		ws := words(u.text, pwsh)
		v := verb(ws)
		if v >= 0 && cdVerbs[strings.ToLower(ws[v])] {
			l, p, ok := "", "", false
			if !frozen && prevAnd && kind == "&&" {
				l, p, ok = cdTarget(logical, ws[v+1:], pwsh)
			}
			if ok {
				logical, cwds, moved = l, uniq(l, p), true
			} else {
				reset()
				frozen = true
			}
		} else if v >= 0 && movesDir(ws[v:]) {
			reset()
			frozen = true
		}
		if kind == ";" {
			reset()
			frozen, prevAnd = false, true
			continue
		}
		// After ||, what follows runs where the chain failed: before or
		// after a cd.
		if kind == "||" && moved {
			reset()
			frozen = true
		}
		prevAnd = kind == "&&"
	}
	return withDirFlags(out, session, pwsh)
}

// unfollowed gives the parts of a line the hook cannot track the session's
// directory and, when anything on the line may change directory, anywhere.
func unfollowed(ps []part, session string, pwsh bool) []part {
	may := []string{session}
	for _, p := range ps {
		ws := words(p.text, pwsh)
		v := verb(ws)
		if cdWord.MatchString(p.text) || v >= 0 && (untracked[strings.ToLower(ws[v])] || pwsh && isScript(ws[v]) || strings.HasPrefix(ws[v], "$")) {
			may = append(may, anywhere)
			break
		}
	}
	for i := range ps {
		ps[i].may = may
	}
	return ps
}

// mayRun sets each part's may. A cd keeps its directory until another cd,
// so after ; or || a command runs where any command of the chain before it
// ran. A cd joined by && moves what follows only if nothing before it may
// have skipped it; otherwise what follows may run on either side of it. A
// cd the hook cannot follow, or anything else that may move, leads
// anywhere for the rest of the line.
func mayRun(out []part, us []unit, session string, pwsh bool) {
	cur, logical := []string{session}, []string{session}
	chain, chainLogical := cur, logical
	lost, prevAnd := false, true
	for _, u := range us {
		may := cur
		if lost {
			may = union(cur, []string{anywhere})
		}
		for k := u.first; k <= u.last; k++ {
			out[k].may = may
		}
		kind := sepKind(u.sep)
		ws := words(u.text, pwsh)
		if v := verb(ws); v >= 0 && cdVerbs[strings.ToLower(ws[v])] {
			var named, dirs []string
			followed := true
			for _, l := range logical {
				n, p, ok := cdTarget(l, ws[v+1:], pwsh)
				if !ok {
					followed = false
					break
				}
				named, dirs = union(named, []string{n}), union(dirs, uniq(n, p))
			}
			if !followed {
				// What follows runs here if the cd failed.
				lost = true
			} else if prevAnd && kind == "&&" {
				cur, logical = dirs, named
			} else {
				cur, logical = union(cur, dirs), union(logical, named)
			}
		} else if v >= 0 && movesDir(ws[v:]) {
			lost = true
		}
		chain, chainLogical = union(chain, cur), union(chainLogical, logical)
		if kind == ";" || kind == "||" {
			cur, logical = chain, chainLogical
		}
		// After ; the next command surely runs.
		prevAnd = kind == "&&" || kind == ";"
	}
}

// union returns a's directories, then b's not in a.
func union(a, b []string) []string {
	out := append([]string{}, a...)
	for _, d := range b {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// unit is a simple command that commandSep may split at a redirect's &, as
// in 2>&1, and the separator after it.
type unit struct {
	text, sep   string
	first, last int // the parts it spans
}

func units(cmd string, seps [][]int) []unit {
	var us []unit
	start, first := 0, 0
	for k := 0; k <= len(seps); k++ {
		end, sep := len(cmd), ""
		if k < len(seps) {
			end, sep = seps[k][0], cmd[seps[k][0]:seps[k][1]]
			if sep == "&" && (end > 0 && strings.IndexByte("<>", cmd[end-1]) >= 0 || seps[k][1] < len(cmd) && cmd[seps[k][1]] == '>') {
				continue
			}
		}
		us = append(us, unit{cmd[start:end], sep, first, k})
		if k < len(seps) {
			start, first = seps[k][1], k+1
		}
	}
	return us
}

// sepKind names what a separator does to the command before it: &&, ||,
// a pipe |, or ends the chain (;, a newline, a background &, anything else).
func sepKind(sep string) string {
	if sep == "&&" || sep == "||" {
		return sep
	}
	if sep == "|" || sep == "|&" {
		return "|"
	}
	return ";"
}

// trackable reports whether the line has nothing that makes where its
// commands run doubtful: no lineDoubt text, no comment, no separator inside
// quotes or after a backslash, no control or non-ASCII character (PowerShell
// reads curly quotes and some line breaks Bash does not), and balanced
// quotes. Then Bash and PowerShell split it where commandSep does.
func trackable(cmd string) bool {
	low := strings.ToLower(cmd)
	for _, s := range lineDoubt {
		if strings.Contains(low, s) {
			return false
		}
	}
	var q byte
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if c >= 0x7f || c < 0x20 && c != '\n' && c != '\t' {
			return false
		}
		if q != 0 {
			if c == q {
				q = 0
			} else if strings.IndexByte(";&|\n", c) >= 0 {
				return false
			}
		} else if c == '\\' && i+1 < len(cmd) && strings.IndexByte(";&|\n#", cmd[i+1]) >= 0 {
			return false
		} else if c == '\'' || c == '"' {
			q = c
		} else if c == '#' {
			return false
		}
	}
	return q == 0
}

// words splits a simple command into words, removing quotes; in Bash an
// unquoted backslash escapes the next character, in PowerShell it is kept.
// trackable has ruled out the escapes the two shells read differently.
func words(s string, pwsh bool) []string {
	var ws []string
	var b strings.Builder
	in := false
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if q != 0 {
			if c == q {
				q = 0
			} else if c == '\\' && q == '"' && !pwsh && i+1 < len(s) && s[i+1] == '\\' {
				b.WriteByte('\\')
				i++
			} else {
				b.WriteByte(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			q, in = c, true
		} else if c == ' ' || c == '\t' || c == '\n' {
			if in {
				ws = append(ws, b.String())
				b.Reset()
				in = false
			}
		} else if c == '\\' && !pwsh && i+1 < len(s) {
			b.WriteByte(s[i+1])
			i++
			in = true
		} else {
			b.WriteByte(c)
			in = true
		}
	}
	if in {
		ws = append(ws, b.String())
	}
	return ws
}

// verb returns the index of the command's name, past assignments and
// keywords, or -1 when there is none.
func verb(ws []string) int {
	for i, w := range ws {
		if !assign.MatchString(w) && !keywords[w] {
			return i
		}
	}
	return -1
}

// isScript reports whether a PowerShell command name may be a script, which
// can change the caller's location.
func isScript(name string) bool {
	low := strings.ToLower(name)
	return strings.ContainsAny(name, `/\`) || strings.HasSuffix(low, ".ps1") || strings.HasSuffix(low, ".psm1")
}

// movesDir reports whether a simple command that is not a followable cd may
// still change the directory: a command run from a variable, popd, or any
// word that looks like a cd (cd.. and cd\ are PowerShell functions; command
// cd and builtin cd are cds too).
func movesDir(ws []string) bool {
	if strings.HasPrefix(ws[0], "$") {
		return true
	}
	for _, w := range ws {
		w = strings.ToLower(w)
		for _, p := range []string{"cd", "chdir", "pushd", "popd"} {
			if strings.HasPrefix(w, p) {
				return true
			}
		}
		if w == "sl" || strings.Contains(w, "location") {
			return true
		}
	}
	return false
}

// cdTarget resolves a cd's arguments from logical, the directory as the
// shell names it. It returns the new directory as named and as resolved
// through symlinks, and false when the cd cannot be followed: no single
// literal argument, an option other than cdFlags, a path the shell would
// expand or read differently, or a directory that is missing or cannot be
// entered.
func cdTarget(logical string, args []string, pwsh bool) (string, string, bool) {
	var dir string
	n, opts := 0, true
	for _, a := range args {
		if opts && a == "--" {
			opts = false
			continue
		}
		if opts && strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+") {
			if !opts || !cdFlags[strings.ToLower(a)] {
				return "", "", false
			}
			continue
		}
		dir = a
		n++
	}
	// A tilde expands only where it begins a word (GNU Bash manual §3.5.2;
	// PowerShell's ~ is a path's first character), so a Windows short name
	// such as RUNNER~1 is literal.
	if n != 1 || dir == "" || strings.ContainsAny(dir, "$*?[%<>|&;") || strings.HasPrefix(dir, "~") {
		return "", "", false
	}
	if pwsh {
		dir = strings.ReplaceAll(dir, `\`, "/")
	} else if strings.Contains(dir, `\`) {
		return "", "", false
	}
	// Git Bash on Windows names drive C: /c.
	if !pwsh && runtime.GOOS == "windows" && len(dir) >= 2 && dir[0] == '/' && isLetter(dir[1]) && (len(dir) == 2 || dir[2] == '/') {
		dir = dir[1:2] + ":/" + dir[min(3, len(dir)):]
	}
	dir = filepath.FromSlash(dir)
	if !filepath.IsAbs(dir) {
		// A drive-relative or rooted path (C:x, \x on Windows) depends on
		// state the hook cannot see, as does CDPATH for a bare name.
		if filepath.VolumeName(dir) != "" || strings.HasPrefix(dir, string(filepath.Separator)) {
			return "", "", false
		}
		if os.Getenv("CDPATH") != "" && dir != "." && dir != ".." && !strings.HasPrefix(dir, "."+string(filepath.Separator)) && !strings.HasPrefix(dir, ".."+string(filepath.Separator)) {
			return "", "", false
		}
		dir = filepath.Join(logical, dir)
	}
	dir = filepath.Clean(dir)
	// Stating dir/. needs search permission on dir, as cd does.
	if fi, err := os.Stat(strings.TrimSuffix(dir, string(filepath.Separator)) + string(filepath.Separator) + "."); err != nil || !fi.IsDir() {
		return "", "", false
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", "", false
	}
	return dir, real, true
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func uniq(a, b string) []string {
	if a == b {
		return []string{a}
	}
	return []string{a, b}
}

// withDirFlags adds, to a part that names the directory a program runs in
// (env -C, make --directory), that directory and the session's, so moving
// the part elsewhere never hides a write there.
func withDirFlags(ps []part, session string, pwsh bool) []part {
	for i, p := range ps {
		ws := words(p.text, pwsh)
		var extra, mayExtra []string
		for k, w := range ws {
			d := ""
			if dirFlags[w] && k+1 < len(ws) {
				d = ws[k+1]
			} else if eq := strings.IndexByte(w, '='); eq > 0 && dirFlags[w[:eq]] {
				d = w[eq+1:]
			}
			if d == "" {
				continue
			}
			if pwsh {
				d = strings.ReplaceAll(d, `\`, "/")
			}
			d = filepath.FromSlash(d)
			for _, c := range p.cwds {
				if filepath.IsAbs(d) {
					extra = append(extra, d)
				} else {
					extra = append(extra, filepath.Join(c, d))
				}
			}
			for _, c := range p.may {
				if filepath.IsAbs(d) {
					mayExtra = append(mayExtra, d)
				} else if c != anywhere {
					mayExtra = append(mayExtra, filepath.Join(c, d))
				}
			}
		}
		if len(extra) > 0 {
			ps[i].cwds = append(append(append([]string{}, p.cwds...), session), extra...)
			ps[i].may = union(p.may, mayExtra)
		}
	}
	return ps
}
