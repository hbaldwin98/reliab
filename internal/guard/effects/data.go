package effects

import "strings"

// The person-only commands must not be found in data, such as a commit
// message that names reliab approve. The data rule blanks text only when it
// is sure Bash never runs it: every simple command on the line prints,
// records, or searches text, or changes directory, and nothing on the line
// is outside the reader's model of Bash. Any other line is judged whole;
// when in doubt, deny. A reviewer's bypasses (a comment hiding a quote or
// heredoc, $'...', a redirect writing a script a later command runs, a
// function or PATH shadowing echo) each meet one of those conditions. A
// substitution keeps a line data only when its own code is all data, as in
// git commit -m "$(cat <<'EOF' … EOF)".

var (
	// dataCommands print or search their arguments and stdin and never run
	// them or store them in a variable (printf -v does). Only bare names
	// count: ./echo could be anything. cd and pushd run nothing.
	dataCommands = map[string]bool{"echo": true, "cat": true, "grep": true, "egrep": true, "fgrep": true, "cd": true, "pushd": true}
	// dataGit are the git subcommands that record or show text; git -c
	// before the subcommand (-c alias.x='!...', core.pager) makes it code.
	dataGit = map[string]bool{"add": true, "commit": true, "tag": true, "notes": true, "status": true, "log": true, "show": true, "diff": true}
	// gitPathOptions are git's global options that take a value and only
	// say where the repository is (git(1) SYNOPSIS).
	gitPathOptions = map[string]bool{"-C": true, "--git-dir": true, "--work-tree": true, "--namespace": true}
	// inertCommands and inertGit run no text they are given: they may
	// share a line with a data command whose text stays data (inert).
	inertCommands = map[string]bool{
		"true": true, "false": true, ":": true, "tail": true, "head": true, "wc": true, "sort": true, "uniq": true,
		"cut": true, "sleep": true, "pwd": true, "ls": true, "go": true,
	}
	inertGit = map[string]bool{
		"push": true, "pull": true, "fetch": true, "status": true, "log": true, "show": true, "diff": true, "add": true,
		"commit": true, "tag": true, "branch": true, "switch": true, "rev-parse": true, "remote": true,
	}
)

// dataArgs returns the index of the first of c's words that is data, or -1
// when c is not a data command. An empty command, such as the one after a
// trailing separator, holds nothing to judge and returns 0.
func dataArgs(c *simple) int {
	ws := c.words
	if len(ws) == 0 {
		if len(c.groups) > 0 {
			return -1
		}
		return 0
	}
	if ws[0].expands || len(ws[0].subs) > 0 {
		return -1
	}
	name := ws[0].text
	if dataCommands[name] {
		return 1
	}
	if name != "git" {
		return -1
	}
	for k := 1; k < len(ws); k++ {
		w := ws[k].text
		opt, _, eq := strings.Cut(w, "=")
		if gitPathOptions[w] {
			k++
		} else if eq && gitPathOptions[opt] || w == "--no-pager" || w == "-P" {
			continue
		} else if dataGit[w] {
			return k + 1
		} else {
			return -1
		}
	}
	return -1
}

// allData reports whether every simple command of a Bash line is a data
// command and none is doubtful.
func allData(l *line) bool {
	if l.sh != Bash {
		return false
	}
	for _, c := range l.cmds {
		if c.doubt || dataArgs(c) < 0 {
			return false
		}
	}
	return true
}

// inert reports whether a command runs no text it is given and holds no
// substitution: it cannot run a data command's text, as bash -c "$(git log
// -1 --format=%B)" or git rebase --exec would. Only bare names count, as
// for data commands.
func inert(c *simple) bool {
	ws := c.words
	if len(ws) == 0 || len(c.groups) > 0 {
		return false
	}
	for _, w := range ws {
		if w.expands || len(w.subs) > 0 {
			return false
		}
	}
	if inertCommands[ws[0].text] {
		return true
	}
	if ws[0].text != "git" {
		return false
	}
	for k := 1; k < len(ws); k++ {
		w := ws[k].text
		opt, _, eq := strings.Cut(w, "=")
		if gitPathOptions[w] {
			k++
		} else if eq && gitPathOptions[opt] || w == "--no-pager" || w == "-P" {
			continue
		} else {
			return inertGit[w]
		}
	}
	return false
}

// dataLine reports whether a Bash line's data commands hold data: no
// command on it is doubtful, and each is a data command or an inert one,
// as in git commit -m "…" && git push, or git commit -m "…" || true. Any
// other command judges the whole line, for it might run the data.
func dataLine(l *line) bool {
	if l.sh != Bash {
		return false
	}
	for _, c := range l.cmds {
		if c.doubt || dataArgs(c) < 0 && !inert(c) {
			return false
		}
	}
	return true
}

// codeText returns the line with its data commands' data blanked out,
// keeping every byte offset, when the line holds data (dataLine); otherwise
// the line unchanged.
func codeText(l *line) string {
	if !dataLine(l) {
		return l.text
	}
	b := []byte(l.text)
	blank := func(from, to int) {
		for i := max(from, 0); i < min(to, len(b)); i++ {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	for _, c := range l.cmds {
		if k := dataArgs(c); k > 0 {
			blank(c.words[k-1].end, c.end)
		}
		for _, bd := range c.bodies {
			blank(bd.start, bd.end)
		}
	}
	return string(b)
}
