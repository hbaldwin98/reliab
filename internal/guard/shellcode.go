package guard

import (
	"regexp"
	"strings"
)

// The person-only guards (findings ack, approve, report verdict, baseline
// write) must not fire on their words inside data, such as a commit message
// that names reliab approve. codeText blanks text only when it is sure Bash
// never runs it: every simple command on the line prints or records text,
// and nothing on the line is outside the scanner's model of Bash. Any other
// line is judged whole; when in doubt, deny. A reviewer's bypasses (a
// comment hiding a quote or heredoc, $'...', a redirect writing a script a
// later command runs, a function or PATH shadowing echo) each meet one of
// those conditions.

// segment is one simple command of a command line.
type segment struct {
	start, end int      // byte range in the command line, separators excluded
	doubt      bool     // holds code Bash expands, or text the scanner may misread
	bodies     [][2]int // heredoc bodies fed to it
}

type heredoc struct {
	delim   string
	strip   bool // <<- strips leading tabs
	literal bool // a quoted delimiter: the body is not expanded
	seg     int
}

// doubtful are the characters outside quotes that make a segment doubtful:
// a comment, a subshell or substitution, a brace group or function body, a
// redirect, and a backtick.
const doubtful = "#(){}>`"

// segments splits a Bash command line into simple commands, honouring quotes
// and heredocs.
func segments(cmd string) []segment {
	var segs []segment
	var pend []heredoc
	cur := segment{}
	inS, inD := false, false
	closeSeg := func(end, next int) {
		cur.end = end
		segs = append(segs, cur)
		cur = segment{start: next}
	}
	// if chains, not switch: Go coverage does not instrument case expressions.
	for i := 0; i < len(cmd); {
		c := cmd[i]
		if inS {
			inS = c != '\''
			i++
		} else if inD {
			if c == '\\' {
				i += 2
				continue
			}
			if c == '"' {
				inD = false
			} else if c == '`' || strings.HasPrefix(cmd[i:], "$(") {
				cur.doubt = true
			}
			i++
		} else if c == '\\' {
			i += 2
		} else if c == '\'' {
			inS = true
			i++
		} else if c == '"' {
			inD = true
			i++
		} else if strings.HasPrefix(cmd[i:], "$'") || strings.IndexByte(doubtful, c) >= 0 {
			cur.doubt = true
			i++
		} else if strings.HasPrefix(cmd[i:], "<<<") {
			i += 3
		} else if strings.HasPrefix(cmd[i:], "<<") {
			h, next := readDelim(cmd, i+2)
			if h.delim != "" {
				h.seg = len(segs)
				pend = append(pend, h)
			}
			i = next
		} else if c == '\n' {
			closeSeg(i, i+1)
			i++
			for _, h := range pend {
				body, next := readBody(cmd, i, h)
				segs[h.seg].bodies = append(segs[h.seg].bodies, body)
				if !h.literal && hasSubst(cmd[body[0]:body[1]]) {
					segs[h.seg].doubt = true
				}
				i = next
			}
			pend = nil
			cur.start = i
		} else if c == '|' || c == ';' || c == '&' {
			next := i + 1
			for next < len(cmd) && strings.IndexByte("|&", cmd[next]) >= 0 {
				next++
			}
			closeSeg(i, next)
			i = next
		} else {
			i++
		}
	}
	closeSeg(len(cmd), len(cmd))
	return segs
}

// readDelim reads a heredoc delimiter starting just after <<.
func readDelim(cmd string, i int) (heredoc, int) {
	var h heredoc
	if i < len(cmd) && cmd[i] == '-' {
		h.strip = true
		i++
	}
	for i < len(cmd) && (cmd[i] == ' ' || cmd[i] == '\t') {
		i++
	}
	var b strings.Builder
	for i < len(cmd) && !strings.ContainsRune(" \t\n;&|<>()", rune(cmd[i])) {
		c := cmd[i]
		if c == '\\' {
			h.literal = true
			if i+1 < len(cmd) {
				b.WriteByte(cmd[i+1])
			}
			i += 2
			continue
		}
		if c != '\'' && c != '"' {
			b.WriteByte(c)
			i++
			continue
		}
		h.literal = true
		end := strings.IndexByte(cmd[i+1:], c)
		if end < 0 {
			return heredoc{}, len(cmd)
		}
		b.WriteString(cmd[i+1 : i+1+end])
		i += end + 2
	}
	h.delim = b.String()
	return h, i
}

// readBody returns the body of h, which starts at i, and the index after its
// closing line. An unterminated body runs to the end. A carriage return is
// ignored on both the delimiter and the line: ending a body early only leaves
// more text to judge.
func readBody(cmd string, i int, h heredoc) ([2]int, int) {
	start := i
	delim := strings.TrimSuffix(h.delim, "\r")
	for i < len(cmd) {
		end := strings.IndexByte(cmd[i:], '\n')
		next := i + end + 1
		if end < 0 {
			end, next = len(cmd)-i, len(cmd)
		}
		line := strings.TrimSuffix(cmd[i:i+end], "\r")
		if h.strip {
			line = strings.TrimLeft(line, "\t")
		}
		if line == delim {
			return [2]int{start, i}, next
		}
		i = next
	}
	return [2]int{start, len(cmd)}, len(cmd)
}

func hasSubst(s string) bool {
	return strings.Contains(s, "$(") || strings.Contains(s, "`")
}

var (
	word = regexp.MustCompile(`\S+`)
	// dataCommands print or search their arguments and stdin and never run
	// them or store them in a variable (printf -v does). Only bare names
	// count: ./echo could be anything.
	dataCommands = map[string]bool{"echo": true, "cat": true, "grep": true, "egrep": true, "fgrep": true}
	// dataGit are the git subcommands that record or show text; git options
	// before the subcommand (-c alias.x='!...') make it code.
	dataGit = map[string]bool{"add": true, "commit": true, "tag": true, "notes": true, "status": true, "log": true, "show": true, "diff": true}
)

// dataArgs returns the offset in s where a data command's arguments start,
// or -1 when s is not a data command. An empty segment, such as the one
// after a trailing separator, holds nothing to judge and returns 0.
func dataArgs(s string) int {
	words := word.FindAllStringIndex(s, -1)
	if len(words) == 0 {
		return 0
	}
	name := s[words[0][0]:words[0][1]]
	if dataCommands[name] {
		return words[0][1]
	}
	if name == "git" && len(words) > 1 && dataGit[s[words[1][0]:words[1][1]]] {
		return words[1][1]
	}
	return -1
}

// codeText returns a Bash command line with its data blanked out, keeping
// every byte offset, when every simple command on it is a data command and
// none is doubtful. Otherwise it returns the line unchanged.
func codeText(cmd string) string {
	segs := segments(cmd)
	offs := make([]int, len(segs))
	for k, g := range segs {
		offs[k] = dataArgs(cmd[g.start:g.end])
		if g.doubt || offs[k] < 0 {
			return cmd
		}
	}
	b := []byte(cmd)
	blank := func(from, to int) {
		for i := from; i < to; i++ {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	for k, g := range segs {
		blank(g.start+offs[k], g.end)
		for _, body := range g.bodies {
			blank(body[0], body[1])
		}
	}
	return string(b)
}
