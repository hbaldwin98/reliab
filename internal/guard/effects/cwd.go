package effects

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
// elsewhere. Surely means a cd the hook can follow (a literal directory
// that exists and can be entered, or that a mkdir earlier in the same &&
// chain makes), joined by && to the commands it moves, in a chain nothing
// on the line can reorder or redefine. Any doubt (a variable, cd -, popd,
// ||, a pipe, a loop, a subshell or brace group, a quote hiding a
// separator, a heredoc, a comment, an alias, eval, a script that may
// change location) leaves the session's directory.
//
// That rule only keeps a write from moving out of the repository. A cd also
// stays in effect past ;, || and a newline, and one the hook cannot follow
// may lead anywhere, so cd internal; rm ../x still writes ../x from
// internal. Each command also carries every directory it may run in, and a
// write is judged from those too.

// Anywhere stands, among a command's directories, for a directory the hook
// cannot know: a cd to a variable or one that may fail, popd, a script that
// may cd.
const Anywhere = "\x00anywhere"

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

// place gives each simple command of l the directories it may run in,
// starting from dirs[0]; any other start directory is added to each
// command's, for code nested in a command that may run in several.
func (r *reader) place(l *line, dirs []string) {
	session := dirs[0]
	r.placeFrom(l, session)
	if len(dirs) > 1 {
		for _, c := range l.cmds {
			c.may = union(c.may, dirs)
		}
	}
}

func (r *reader) placeFrom(l *line, session string) {
	pwsh := l.sh == PowerShell
	cmds := l.cmds
	for _, c := range cmds {
		c.cwds, c.may = []string{session}, []string{session}
	}
	if !trackable(joined(l.text, l.sh)) {
		unfollowed(l, session, pwsh)
		withDirFlags(cmds, session, pwsh)
		return
	}
	for _, c := range cmds {
		ws := c.texts()
		if v := verb(ws); v >= 0 && (untracked[strings.ToLower(ws[v])] || pwsh && isScript(ws[v])) {
			unfollowed(l, session, pwsh)
			withDirFlags(cmds, session, pwsh)
			return
		}
	}
	made(cmds, session, pwsh)
	r.mayRun(cmds, session, pwsh)
	session1 := []string{session}
	logical, cwds := session, session1
	moved, frozen, prevAnd := false, false, true
	reset := func() { logical, cwds, moved = session, session1, false }
	for _, c := range cmds {
		c.cwds = cwds
		kind := sepKind(c.sep)
		ws := c.texts()
		v := verb(ws)
		if v >= 0 && cdVerbs[strings.ToLower(ws[v])] {
			l, p, ok := "", "", false
			if !frozen && prevAnd && kind == "&&" {
				l, p, ok = r.cdTarget(logical, ws[v+1:], pwsh, c)
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
	withDirFlags(cmds, session, pwsh)
}

// joined is the line with its continuations joined, as the shell joins
// them (GNU Bash manual §3.1.2.1; a PowerShell backtick before a newline),
// each byte kept in place.
func joined(s string, sh Shell) string {
	esc := `\`
	if sh == PowerShell {
		esc = "`"
	}
	if !strings.Contains(s, esc+"\n") && !strings.Contains(s, esc+"\r\n") {
		return s
	}
	return strings.NewReplacer(esc+"\r\n", "   ", esc+"\n", "  ").Replace(s)
}

// unfollowed gives the commands of a line the hook cannot track the
// session's directory and, when anything on the line may change directory,
// anywhere.
func unfollowed(l *line, session string, pwsh bool) {
	may := []string{session}
	moves := mayHoldCd(l.text) && cdWord.MatchString(l.text) || l.any(func(c *simple) bool {
		ws := c.texts()
		v := verb(ws)
		return v >= 0 && (untracked[strings.ToLower(ws[v])] || pwsh && isScript(ws[v]) || strings.HasPrefix(ws[v], "$"))
	})
	if moves {
		may = append(may, Anywhere)
	}
	for _, c := range l.cmds {
		c.may = may
	}
}

// mayHoldCd reports whether s holds text cdWord could match, which is
// cheaper to ask first: every name it matches holds cd, pushd, popd, sl,
// or location.
func mayHoldCd(s string) bool {
	low := strings.ToLower(s)
	for _, w := range []string{"cd", "pushd", "popd", "sl", "location"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// made records, on each command of an && chain, the directories a mkdir
// earlier in the chain made: a cd there follows, though the directory does
// not exist when the hook runs. Only absolute paths count, so where the
// mkdir ran does not matter.
func made(cmds []*simple, session string, pwsh bool) {
	var dirs map[string]bool
	for _, c := range cmds {
		c.made = dirs
		kind := sepKind(c.sep)
		if kind != "&&" {
			dirs = nil
			continue
		}
		ws := c.texts()
		v := verb(ws)
		if v < 0 || !mkdirVerbs[strings.ToLower(ws[v])] {
			continue
		}
		next := map[string]bool{}
		for d := range dirs {
			next[d] = true
		}
		parents := false
		for _, a := range ws[v+1:] {
			if a == "-p" || a == "--parents" || strings.EqualFold(a, "-force") {
				parents = true
			}
		}
		for _, a := range ws[v+1:] {
			if strings.HasPrefix(a, "-") {
				continue
			}
			d, ok := resolve(session, a, pwsh)
			if !ok || !absArg(a, pwsh) {
				continue
			}
			next[d] = true
			for p := filepath.Dir(d); parents && p != d && p != filepath.Dir(p); p = filepath.Dir(p) {
				next[p] = true
				d = p
			}
		}
		dirs = next
	}
}

// mkdirVerbs make the directories they name.
var mkdirVerbs = map[string]bool{"mkdir": true, "md": true}

// absArg reports whether a command's path argument is absolute.
func absArg(a string, pwsh bool) bool {
	if pwsh {
		a = strings.ReplaceAll(a, `\`, "/")
	}
	return isDrivePath(a, pwsh) || filepath.IsAbs(filepath.FromSlash(a))
}

// isDrivePath reports whether a is a Git Bash drive path such as /c/x.
func isDrivePath(a string, pwsh bool) bool {
	return !pwsh && runtime.GOOS == "windows" && len(a) >= 2 && a[0] == '/' && isLetter(a[1]) && (len(a) == 2 || a[2] == '/')
}

// mayRun sets each command's may. A cd keeps its directory until another
// cd, so after ; or || a command runs where any command of the chain before
// it ran. A cd joined by && moves what follows only if nothing before it
// may have skipped it; otherwise what follows may run on either side of
// it. A cd the hook cannot follow, or anything else that may move, leads
// anywhere for the rest of the line.
func (r *reader) mayRun(cmds []*simple, session string, pwsh bool) {
	cur, logical := []string{session}, []string{session}
	chain, chainLogical := cur, logical
	lost, prevAnd := false, true
	for _, c := range cmds {
		may := cur
		if lost {
			may = union(cur, []string{Anywhere})
		}
		c.may = may
		kind := sepKind(c.sep)
		ws := c.texts()
		if v := verb(ws); v >= 0 && cdVerbs[strings.ToLower(ws[v])] {
			var named, dirs []string
			followed := true
			for _, l := range logical {
				n, p, ok := r.cdTarget(l, ws[v+1:], pwsh, c)
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
// quotes. Then Bash and PowerShell split it where the reader does.
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
// literal argument, an option other than cdFlags, a redirection, a path
// the shell would expand or read differently, or a directory that is
// missing (and no earlier mkdir made) or cannot be entered.
func (r *reader) cdTarget(logical string, args []string, pwsh bool, c *simple) (string, string, bool) {
	if c.redirs > 0 {
		return "", "", false
	}
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
	if n != 1 {
		return "", "", false
	}
	dir, ok := resolve(logical, dir, pwsh)
	if !ok {
		return "", "", false
	}
	real, ok := r.enter(dir)
	if !ok && c.made[dir] {
		return dir, existing(dir), true
	}
	return dir, real, ok
}

// resolve turns a cd's literal argument into the directory it names from
// logical, or false when the shell would expand or read it differently.
func resolve(logical, dir string, pwsh bool) (string, bool) {
	// A tilde expands only where it begins a word (GNU Bash manual §3.5.2;
	// PowerShell's ~ is a path's first character), so a Windows short name
	// such as RUNNER~1 is literal.
	if dir == "" || strings.ContainsAny(dir, "$*?[%<>|&;") || strings.HasPrefix(dir, "~") {
		return "", false
	}
	if pwsh {
		dir = strings.ReplaceAll(dir, `\`, "/")
	} else if strings.Contains(dir, `\`) {
		return "", false
	}
	// Git Bash on Windows names drive C: /c.
	if isDrivePath(dir, pwsh) {
		dir = dir[1:2] + ":/" + dir[min(3, len(dir)):]
	}
	dir = filepath.FromSlash(dir)
	if !filepath.IsAbs(dir) {
		// A drive-relative or rooted path (C:x, \x on Windows) depends on
		// state the hook cannot see, as does CDPATH for a bare name.
		if filepath.VolumeName(dir) != "" || strings.HasPrefix(dir, string(filepath.Separator)) {
			return "", false
		}
		if os.Getenv("CDPATH") != "" && dir != "." && dir != ".." && !strings.HasPrefix(dir, "."+string(filepath.Separator)) && !strings.HasPrefix(dir, ".."+string(filepath.Separator)) {
			return "", false
		}
		dir = filepath.Join(logical, dir)
	}
	return filepath.Clean(dir), true
}

type entered struct {
	real string
	ok   bool
}

// enter reports whether a cd can enter dir, and where symlinks lead: a
// directory that exists, with search permission (stating dir/. needs it,
// as cd does). The answer is kept for the rest of the read.
func (r *reader) enter(dir string) (string, bool) {
	if e, ok := r.entered[dir]; ok {
		return e.real, e.ok
	}
	var e entered
	if fi, err := os.Stat(strings.TrimSuffix(dir, string(filepath.Separator)) + string(filepath.Separator) + "."); err == nil && fi.IsDir() {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			e = entered{real, true}
		}
	}
	if r.entered == nil {
		r.entered = map[string]entered{}
	}
	r.entered[dir] = e
	return e.real, e.ok
}

// existing resolves p through symlinks as far as it exists, and joins the
// rest as named.
func existing(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	dir := filepath.Dir(p)
	if dir == p {
		return p
	}
	return filepath.Join(existing(dir), filepath.Base(p))
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func uniq(a, b string) []string {
	if a == b {
		return []string{a}
	}
	return []string{a, b}
}

// withDirFlags adds, to a command that names the directory a program runs
// in (env -C, make --directory), that directory and the session's, so
// moving the command elsewhere never hides a write there.
func withDirFlags(cmds []*simple, session string, pwsh bool) {
	for _, c := range cmds {
		ws := c.texts()
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
			for _, cw := range c.cwds {
				if filepath.IsAbs(d) {
					extra = append(extra, d)
				} else {
					extra = append(extra, filepath.Join(cw, d))
				}
			}
			for _, m := range c.may {
				if filepath.IsAbs(d) {
					mayExtra = append(mayExtra, d)
				} else if m != Anywhere {
					mayExtra = append(mayExtra, filepath.Join(m, d))
				}
			}
		}
		if len(extra) > 0 {
			c.cwds = append(append(append([]string{}, c.cwds...), session), extra...)
			c.may = union(c.may, mayExtra)
		}
	}
}
