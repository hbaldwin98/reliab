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

// codeText returns the line with its data blanked out, keeping every byte
// offset, when the line is all data; otherwise the line unchanged.
func codeText(l *line) string {
	if !allData(l) {
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
