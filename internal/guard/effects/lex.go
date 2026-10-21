package effects

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The lexer reads a command line once, the way its shell does: words past
// quotes and escapes (GNU Bash manual §3.1.2; PowerShell about_Quoting_Rules
// and about_Special_Characters), line continuations (§3.1.2.1; a PowerShell
// backtick before a newline), separators (§3.2.4 Lists), redirections
// (§3.6), heredocs (§3.6.6), comments, and the code nested in a word:
// command and process substitution (§3.5.4, §3.5.6), subshells and
// PowerShell's groups and script blocks. PowerShell also reads curly quotes
// as quotes and a no-break space as a space. Everything after it reads the
// words, never the raw text.

// maxDepth bounds code nested in code, so a pathological line still ends.
const maxDepth = 12

// word is one word of a simple command after quote removal.
type word struct {
	text       string
	start, end int  // span in the line it was read from
	quoted     bool // some of it was quoted or escaped
	expands    bool // holds a parameter expansion or substitution
	subs       []*line
}

// body is text fed to a command's standard input: a heredoc's body or a
// here-string.
type body struct {
	text       string
	start, end int // span in the line; a here-string's is its word's
	subs       []*line
}

// simple is one simple command, or a subshell or group standing for one.
type simple struct {
	words      []word
	outs       []string // files its redirections write
	sep        string   // the separator after it
	start, end int      // span in the line, separator excluded
	bodies     []body
	groups     []*line // subshells, groups, and script blocks it is or holds
	// doubt says the command holds text the data rule distrusts (see
	// data.go): a comment, a subshell or brace, an output redirect, $'…',
	// or a substitution that is not itself all data.
	doubt bool
	// cwds and may are the directories it runs in as far as the hook can
	// follow, and every directory it may run in (cwd.go); made holds the
	// directories a mkdir earlier in its && chain made.
	cwds, may []string
	made      map[string]bool
	redirs    int // redirections it has
}

// line is a lexed command line.
type line struct {
	text     string
	sh       Shell
	cmds     []*simple
	comments []string
}

type heredoc struct {
	delim   string
	strip   bool // <<- strips leading tabs
	literal bool // a quoted delimiter: the body is not expanded
	cmd     *simple
}

// redirect operand kinds.
const (
	opNone   = iota
	opWrite  // the next word is a file written
	opRead   // the next word is a file read or a descriptor
	opDup    // >&word: a file unless word is a number or -
	opString // <<< word: a here-string
)

type lexer struct {
	s       string
	i       int
	sh      Shell
	depth   int
	close   byte // the byte that ends this nested line, or 0
	ln      *line
	cur     *simple
	b       strings.Builder
	inWord  bool
	wq, wx  bool
	wstart  int
	wsubs   []*line
	op      int
	pending []heredoc
}

// lex reads s as a whole line.
func lex(s string, sh Shell, depth int) *line {
	l := &lexer{s: s, sh: sh, depth: depth}
	l.run()
	return l.ln
}

// nested reads the code that starts at i and ends at an unmatched close
// byte, as in $(…), (…), or a PowerShell {…}. It returns the line and the
// index after the close byte (or the end).
func (l *lexer) nested(i int, close byte) (*line, int) {
	n := &lexer{s: l.s, i: i, sh: l.sh, depth: l.depth + 1, close: close}
	if n.depth > maxDepth {
		// Too deep to read: keep the text as one doubtful word.
		end := matchClose(l.s, i, close)
		ln := &line{text: l.s[i:end], sh: l.sh, cmds: []*simple{{words: []word{{text: l.s[i:end], start: i, end: end}}, start: i, end: end, doubt: true}}}
		return ln, min(end+1, len(l.s))
	}
	n.run()
	return n.ln, n.i
}

// matchClose finds the close byte that ends text opened at i, counting
// nesting and skipping quotes; the end of s when there is none.
func matchClose(s string, i int, close byte) int {
	open := byte('(')
	if close == '}' {
		open = '{'
	}
	depth := 0
	for ; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' || c == '`':
			i++
		case c == '\'' || c == '"':
			if e := strings.IndexByte(s[i+1:], c); e >= 0 {
				i += e + 1
			} else {
				return len(s)
			}
		case c == open:
			depth++
		case c == close:
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return len(s)
}

func (l *lexer) run() {
	l.ln = &line{sh: l.sh}
	start := l.i
	l.cur = &simple{start: l.i}
	for l.i < len(l.s) {
		if l.close != 0 && l.s[l.i] == l.close {
			l.endCmd(l.i, "")
			l.ln.text = l.s[start:l.i]
			l.i++
			return
		}
		if l.sh == PowerShell {
			l.stepPwsh()
		} else {
			l.stepBash()
		}
	}
	l.endCmd(len(l.s), "")
	l.ln.text = l.s[start:]
}

// endCmd finishes the current word and simple command.
func (l *lexer) endCmd(end int, sep string) {
	l.endWord()
	l.op = opNone
	l.cur.end = end
	l.cur.sep = sep
	l.ln.cmds = append(l.ln.cmds, l.cur)
	l.cur = &simple{start: end + len(sep)}
}

// sep ends the simple command at i with a separator n bytes long, and
// reads any heredoc bodies a newline starts.
func (l *lexer) sep(n int) {
	sep := l.s[l.i : l.i+n]
	l.endCmd(l.i, sep)
	l.i += n
	l.cur.start = l.i
	if sep == "\n" && len(l.pending) > 0 {
		for _, h := range l.pending {
			b, next := readBody(l.s, l.i, h)
			bd := body{text: l.s[b[0]:b[1]], start: b[0], end: b[1]}
			if !h.literal {
				bd.subs = l.bodySubs(b[0], b[1])
				for _, sub := range bd.subs {
					if !allData(sub) {
						h.cmd.doubt = true
					}
				}
			}
			h.cmd.bodies = append(h.cmd.bodies, bd)
			l.i = next
		}
		l.pending = nil
		l.cur.start = l.i
	}
}

// bodySubs reads the substitutions in an expanded heredoc body.
func (l *lexer) bodySubs(from, to int) []*line {
	var subs []*line
	for i := from; i < to; i++ {
		c := l.s[i]
		if c == '\\' {
			i++
		} else if c == '$' && i+1 < to && l.s[i+1] == '(' {
			sub, next := l.nested(i+2, ')')
			subs = append(subs, sub)
			i = next - 1
		} else if c == '`' {
			sub, next := l.backtick(i)
			subs = append(subs, sub)
			i = next - 1
		}
	}
	return subs
}

func (l *lexer) startWord() {
	if !l.inWord {
		l.inWord = true
		l.wstart = l.i
	}
}

// endWord finishes the current word: a redirect's operand, or the next
// word of the command.
func (l *lexer) endWord() {
	if !l.inWord {
		return
	}
	w := word{text: l.b.String(), start: l.wstart, end: l.i, quoted: l.wq, expands: l.wx, subs: l.wsubs}
	l.b.Reset()
	l.inWord, l.wq, l.wx, l.wsubs = false, false, false, nil
	op := l.op
	l.op = opNone
	if op != opNone {
		l.cur.redirs++
	}
	switch op {
	case opWrite:
		l.cur.outs = append(l.cur.outs, w.text)
	case opDup:
		if !isDescriptor(w.text) {
			l.cur.outs = append(l.cur.outs, w.text)
		}
	case opString:
		l.cur.bodies = append(l.cur.bodies, body{text: w.text, start: w.start, end: w.end, subs: w.subs})
	case opRead:
	default:
		if l.sh != PowerShell && !w.quoted && (w.text == "{" || w.text == "}") {
			l.cur.doubt = true
		}
		l.cur.words = append(l.cur.words, w)
	}
	l.trackSubs(w.subs)
}

// trackSubs marks the command doubtful for a substitution that is not all
// data (data.go).
func (l *lexer) trackSubs(subs []*line) {
	for _, s := range subs {
		if !allData(s) {
			l.cur.doubt = true
		}
	}
}

// isDescriptor reports whether a >& operand names a descriptor (a number,
// or - to close one) rather than a file (GNU Bash manual §3.6.8).
func isDescriptor(s string) bool {
	if s == "-" {
		return true
	}
	s = strings.TrimSuffix(s, "-")
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// fdPrefix reports whether the word read so far is a redirect's descriptor
// number (2>), or PowerShell's * (*>) or Bash's & (&>), and drops it.
func (l *lexer) fdPrefix() {
	if !l.inWord || l.wq || l.wx || len(l.wsubs) > 0 {
		l.endWord()
		return
	}
	t := l.b.String()
	ok := t != ""
	for i := 0; i < len(t); i++ {
		if (t[i] < '0' || t[i] > '9') && !(t == "*" && l.sh == PowerShell) {
			ok = false
		}
	}
	if ok {
		l.b.Reset()
		l.inWord = false
		return
	}
	l.endWord()
}

func (l *lexer) appendRune() {
	r, n := utf8.DecodeRuneInString(l.s[l.i:])
	if r == utf8.RuneError && n <= 1 {
		l.b.WriteByte(l.s[l.i])
		l.i++
		return
	}
	l.b.WriteString(l.s[l.i : l.i+n])
	l.i += n
}

func (l *lexer) stepBash() {
	s, c := l.s, l.s[l.i]
	switch {
	case c == ' ' || c == '\t':
		l.endWord()
		l.i++
	case c == '\\':
		if strings.HasPrefix(s[l.i:], "\\\n") {
			l.i += 2
		} else if strings.HasPrefix(s[l.i:], "\\\r\n") {
			l.i += 3
		} else if l.i+1 < len(s) {
			l.startWord()
			l.wq = true
			l.i++
			l.appendRune()
		} else {
			l.i++
		}
	case c == '\'':
		l.startWord()
		l.wq = true
		end := strings.IndexByte(s[l.i+1:], '\'')
		if end < 0 {
			l.b.WriteString(s[l.i+1:])
			l.i = len(s)
		} else {
			l.b.WriteString(s[l.i+1 : l.i+1+end])
			l.i += end + 2
		}
	case c == '"':
		l.startWord()
		l.wq = true
		l.i++
		l.doubleBash()
	case c == '$':
		l.dollar()
	case c == '`':
		l.startWord()
		l.wx = true
		sub, next := l.backtick(l.i)
		l.b.WriteString(s[l.i:next])
		l.wsubs = append(l.wsubs, sub)
		l.i = next
	case c == '(':
		if l.inWord && strings.HasSuffix(l.b.String(), "=") && !l.wq {
			// An array assignment, a=(x y): one word.
			end := matchClose(s, l.i+1, ')')
			l.b.WriteString(s[l.i:min(end+1, len(s))])
			l.i = min(end+1, len(s))
			return
		}
		l.endWord()
		l.cur.doubt = true
		g, next := l.nested(l.i+1, ')')
		l.cur.groups = append(l.cur.groups, g)
		l.i = next
	case c == ')':
		l.cur.doubt = true
		l.sep(1)
	case c == '\n':
		l.sep(1)
	case c == ';':
		n := 1
		for l.i+n < len(s) && (s[l.i+n] == ';' || s[l.i+n] == '&') {
			n++
		}
		l.sep(n)
	case c == '|':
		if strings.HasPrefix(s[l.i:], "||") || strings.HasPrefix(s[l.i:], "|&") {
			l.sep(2)
		} else {
			l.sep(1)
		}
	case c == '&':
		if strings.HasPrefix(s[l.i:], "&&") {
			l.sep(2)
		} else if strings.HasPrefix(s[l.i:], "&>") {
			l.endWord()
			l.cur.doubt = true
			l.i += 2
			if l.i < len(s) && s[l.i] == '>' {
				l.i++
			}
			l.op = opWrite
		} else {
			l.sep(1)
		}
	case c == '>':
		l.fdPrefix()
		l.cur.doubt = true
		if strings.HasPrefix(s[l.i:], ">(") {
			l.processSub()
			return
		}
		l.i++
		if l.i < len(s) && (s[l.i] == '>' || s[l.i] == '|') {
			l.i++
			l.op = opWrite
		} else if l.i < len(s) && s[l.i] == '&' {
			l.i++
			l.op = opDup
		} else {
			l.op = opWrite
		}
	case c == '<':
		l.fdPrefix()
		l.lessThan()
	case c == '#' && !l.inWord:
		l.comment()
	default:
		l.startWord()
		if c == '{' || c == '}' {
			// Brace expansion or a brace group (§3.5.1, §3.2.5.3).
			l.cur.doubt = true
		}
		l.appendRune()
	}
}

// lessThan reads a redirect that starts with <.
func (l *lexer) lessThan() {
	s := l.s
	switch {
	case strings.HasPrefix(s[l.i:], "<<<"):
		l.i += 3
		l.op = opString
	case strings.HasPrefix(s[l.i:], "<<"):
		h, next := readDelim(s, l.i+2)
		l.cur.redirs++
		if h.delim != "" {
			h.cmd = l.cur
			l.pending = append(l.pending, h)
		}
		l.i = next
	case strings.HasPrefix(s[l.i:], "<("):
		l.processSub()
	case strings.HasPrefix(s[l.i:], "<>"):
		l.i += 2
		l.op = opWrite
	case strings.HasPrefix(s[l.i:], "<&"):
		l.i += 2
		l.op = opRead
	default:
		l.i++
		l.op = opRead
	}
}

// processSub reads <(…) or >(…) as a word whose code runs.
func (l *lexer) processSub() {
	l.endWord()
	l.startWord()
	l.wx = true
	start := l.i
	sub, next := l.nested(l.i+2, ')')
	l.b.WriteString(l.s[start:next])
	l.wsubs = append(l.wsubs, sub)
	l.i = next
	l.endWord()
}

func (l *lexer) comment() {
	end := strings.IndexByte(l.s[l.i:], '\n')
	if end < 0 {
		end = len(l.s) - l.i
	}
	if l.close != 0 {
		if c := strings.IndexByte(l.s[l.i:l.i+end], l.close); c >= 0 {
			end = c
		}
	}
	l.ln.comments = append(l.ln.comments, l.s[l.i+1:l.i+end])
	l.cur.doubt = true
	l.i += end
}

// doubleBash reads a double-quoted string's contents after the opening
// quote (GNU Bash manual §3.1.2.3).
func (l *lexer) doubleBash() {
	s := l.s
	for l.i < len(s) {
		c := s[l.i]
		switch {
		case c == '"':
			l.i++
			return
		case c == '\\' && l.i+1 < len(s):
			switch s[l.i+1] {
			case '\n':
				l.i += 2
			case '$', '`', '"', '\\':
				l.b.WriteByte(s[l.i+1])
				l.i += 2
			default:
				l.b.WriteByte('\\')
				l.i++
			}
		case c == '$' && l.i+1 < len(s) && s[l.i+1] == '(':
			l.wx = true
			start := l.i
			sub, next := l.nested(l.i+2, ')')
			l.b.WriteString(s[start:next])
			l.wsubs = append(l.wsubs, sub)
			l.i = next
		case c == '$' && l.i+1 < len(s) && s[l.i+1] == '{':
			l.wx = true
			end := matchClose(s, l.i+2, '}')
			l.b.WriteString(s[l.i:min(end+1, len(s))])
			l.i = min(end+1, len(s))
		case c == '$':
			l.wx = true
			l.b.WriteByte(c)
			l.i++
		case c == '`':
			l.wx = true
			sub, next := l.backtick(l.i)
			l.b.WriteString(s[l.i:next])
			l.wsubs = append(l.wsubs, sub)
			l.i = next
		default:
			l.appendRune()
		}
	}
}

// dollar reads what a $ starts outside quotes.
func (l *lexer) dollar() {
	s := l.s
	l.startWord()
	next := byte(0)
	if l.i+1 < len(s) {
		next = s[l.i+1]
	}
	switch {
	case next == '\'':
		// ANSI-C quoting (§3.1.2.4).
		l.wq = true
		l.cur.doubt = true
		l.i += 2
		for l.i < len(s) && s[l.i] != '\'' {
			if s[l.i] == '\\' && l.i+1 < len(s) {
				l.b.WriteByte(ansiC(s[l.i+1]))
				l.i += 2
				continue
			}
			l.appendRune()
		}
		l.i = min(l.i+1, len(s))
	case next == '"':
		l.wq = true
		l.i += 2
		l.doubleBash()
	case next == '(':
		l.wx = true
		start := l.i
		sub, end := l.nested(l.i+2, ')')
		l.b.WriteString(s[start:end])
		l.wsubs = append(l.wsubs, sub)
		l.i = end
	case next == '{':
		l.wx = true
		l.cur.doubt = true
		end := matchClose(s, l.i+2, '}')
		l.b.WriteString(s[l.i:min(end+1, len(s))])
		l.i = min(end+1, len(s))
	case next == '_' || next == '@' || next == '*' || next == '#' || next == '?' || next == '!' || next == '$' || next == '-' ||
		next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' || next >= '0' && next <= '9':
		l.wx = true
		l.b.WriteByte('$')
		l.i++
	default:
		l.b.WriteByte('$')
		l.i++
	}
}

func ansiC(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	}
	return c
}

// backtick reads `…` starting at i, a substitution whose backslashes
// escape only $, `, and \ (§3.5.4), and returns its code and the index
// after it.
func (l *lexer) backtick(i int) (*line, int) {
	s := l.s
	var b strings.Builder
	j := i + 1
	for j < len(s) && s[j] != '`' {
		if s[j] == '\\' && j+1 < len(s) && strings.IndexByte("$`\\", s[j+1]) >= 0 {
			b.WriteByte(s[j+1])
			j += 2
			continue
		}
		b.WriteByte(s[j])
		j++
	}
	end := min(j+1, len(s))
	if l.depth+1 > maxDepth {
		return &line{text: b.String(), sh: l.sh, cmds: []*simple{{words: []word{{text: b.String()}}, doubt: true}}}, end
	}
	return lex(b.String(), l.sh, l.depth+1), end
}

// pwshQuote reports whether r is one of PowerShell's single (1) or double
// (2) quotes: ASCII or curly (about_Quoting_Rules).
func pwshQuote(r rune) int {
	switch r {
	case '\'', '‘', '’', '‚', '‛':
		return 1
	case '"', '“', '”', '„':
		return 2
	}
	return 0
}

// pwshSpace reports whether PowerShell reads r as whitespace: a space, a
// tab, a no-break space, or any other Unicode space separator.
func pwshSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\v' || r == '\f' || r == ' ' || r != '\n' && r != '\r' && unicode.Is(unicode.Zs, r)
}

func (l *lexer) stepPwsh() {
	s := l.s
	r, n := utf8.DecodeRuneInString(s[l.i:])
	c := s[l.i]
	switch {
	case pwshSpace(r):
		l.endWord()
		l.i += n
	case c == '`':
		if strings.HasPrefix(s[l.i:], "`\n") {
			l.i += 2
		} else if strings.HasPrefix(s[l.i:], "`\r\n") {
			l.i += 3
		} else if l.i+1 < len(s) {
			l.startWord()
			l.wq = true
			l.i++
			l.appendRune()
		} else {
			l.i++
		}
	case pwshQuote(r) == 1:
		l.startWord()
		l.wq = true
		l.i += n
		l.singlePwsh()
	case pwshQuote(r) == 2:
		l.startWord()
		l.wq = true
		l.i += n
		l.doublePwsh()
	case c == '@' && !l.inWord && l.hereString():
	case c == '$' || c == '@':
		l.startWord()
		if l.i+1 < len(s) && (s[l.i+1] == '(' || s[l.i+1] == '{') {
			l.i++
			l.group()
			return
		}
		if c == '$' {
			l.wx = true
		}
		l.appendRune()
	case c == '(' || c == '{':
		l.startWord()
		l.group()
	case c == ')' || c == '}':
		l.sep(1)
	case c == '\n' || c == '\r':
		if strings.HasPrefix(s[l.i:], "\r\n") {
			l.sep(2)
		} else {
			l.sep(1)
		}
	case c == ';':
		l.sep(1)
	case c == '|':
		if strings.HasPrefix(s[l.i:], "||") {
			l.sep(2)
		} else {
			l.sep(1)
		}
	case c == '&':
		if strings.HasPrefix(s[l.i:], "&&") {
			l.sep(2)
		} else if !l.inWord && len(l.cur.words) == 0 {
			// The call operator (about_Operators).
			l.startWord()
			l.appendRune()
			l.endWord()
		} else {
			l.sep(1)
		}
	case c == '>':
		l.fdPrefix()
		l.i++
		if l.i < len(s) && s[l.i] == '>' {
			l.i++
			l.op = opWrite
		} else if l.i < len(s) && s[l.i] == '&' {
			l.i++
			l.op = opDup
		} else {
			l.op = opWrite
		}
	case c == '<' && strings.HasPrefix(s[l.i:], "<#") && !l.inWord:
		end := strings.Index(s[l.i:], "#>")
		if end < 0 {
			end = len(s) - l.i
		} else {
			end += 2
		}
		l.ln.comments = append(l.ln.comments, s[l.i:l.i+end])
		l.i += end
	case c == '<':
		l.fdPrefix()
		l.i++
		l.op = opRead
	case c == '#' && !l.inWord:
		l.comment()
	case !l.inWord && (r == '–' || r == '—' || r == '―'):
		// PowerShell reads a dash as a hyphen before a parameter name.
		l.startWord()
		l.b.WriteByte('-')
		l.i += n
	default:
		l.startWord()
		l.appendRune()
	}
}

// group reads (…) or {…} at l.i, PowerShell's grouping, subexpression,
// array, script block, or hash table, as part of the current word.
func (l *lexer) group() {
	close := byte(')')
	if l.s[l.i] == '{' {
		close = '}'
	}
	start := l.i
	if start > 0 && (l.s[start-1] == '$' || l.s[start-1] == '@') {
		// $( and @( keep their sigil in the word's text.
		start--
	}
	g, next := l.nested(l.i+1, close)
	l.b.WriteString(l.s[start:next])
	l.wsubs = append(l.wsubs, g)
	l.i = next
}

func (l *lexer) singlePwsh() {
	s := l.s
	for l.i < len(s) {
		r, n := utf8.DecodeRuneInString(s[l.i:])
		if pwshQuote(r) == 1 {
			if r2, n2 := utf8.DecodeRuneInString(s[l.i+n:]); l.i+n < len(s) && pwshQuote(r2) == 1 {
				l.b.WriteByte('\'')
				l.i += n + n2
				continue
			}
			l.i += n
			return
		}
		l.appendRune()
	}
}

func (l *lexer) doublePwsh() {
	s := l.s
	for l.i < len(s) {
		r, n := utf8.DecodeRuneInString(s[l.i:])
		switch {
		case pwshQuote(r) == 2:
			if r2, n2 := utf8.DecodeRuneInString(s[l.i+n:]); l.i+n < len(s) && pwshQuote(r2) == 2 {
				l.b.WriteByte('"')
				l.i += n + n2
				continue
			}
			l.i += n
			return
		case r == '`' && l.i+1 < len(s):
			l.i++
			l.appendRune()
		case r == '$' && l.i+1 < len(s) && s[l.i+1] == '(':
			l.wx = true
			start := l.i
			sub, next := l.nested(l.i+2, ')')
			l.b.WriteString(s[start:next])
			l.wsubs = append(l.wsubs, sub)
			l.i = next
		case r == '$':
			l.wx = true
			l.appendRune()
		default:
			l.appendRune()
		}
	}
}

// hereString reads a PowerShell here-string, @'…'@ or @"…"@, whose
// opening quote ends its line and whose closing quote starts one
// (about_Quoting_Rules). It reports whether there was one.
func (l *lexer) hereString() bool {
	s := l.s
	r, n := utf8.DecodeRuneInString(s[l.i+1:])
	kind := pwshQuote(r)
	if kind == 0 {
		return false
	}
	j := l.i + 1 + n
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	if j < len(s) && s[j] == '\r' {
		j++
	}
	if j >= len(s) || s[j] != '\n' {
		return false
	}
	l.startWord()
	l.wq = true
	from := j + 1
	end, next := len(s), len(s)
	for k := from; k < len(s); {
		if r, m := utf8.DecodeRuneInString(s[k:]); pwshQuote(r) == kind && k+m < len(s) && s[k+m] == '@' {
			end, next = k, k+m+1
			break
		}
		nl := strings.IndexByte(s[k:], '\n')
		if nl < 0 {
			break
		}
		k += nl + 1
	}
	text := strings.TrimSuffix(strings.TrimSuffix(s[from:min(end, len(s))], "\n"), "\r")
	l.b.WriteString(text)
	if kind == 2 {
		for i := from; i < end; i++ {
			if s[i] == '$' && i+1 < end && s[i+1] == '(' {
				sub, after := l.nested(i+2, ')')
				l.wsubs = append(l.wsubs, sub)
				l.wx = true
				i = after - 1
			}
		}
	}
	l.i = next
	return true
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
	return h, min(i, len(cmd))
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
