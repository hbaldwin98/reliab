// Package effects reads a shell command line and says what it may do: the
// simple commands it runs, with their words read once as the shell reads
// them; the directories each may run in; the paths each may write and the
// directories each may rewrite or remove whole; the reliab command paths
// the line may invoke; and whether it pushes. The guard judges these
// effects against its policies (knowledge/decisions/command-effects.md);
// nothing here knows which paths are protected.
//
// One reader serves every judgement, so a respelling the reader learns
// (a continuation, a quote, an escape, a curly quote, code nested in
// bash -c or iex) is learnt for writes and invocations alike.
package effects

import (
	"path/filepath"
	"strings"
)

// Shell is the language a line is read in.
type Shell int

const (
	// Bash reads a line as GNU Bash does; its data rule applies (data.go).
	Bash Shell = iota
	// PowerShell reads a line as PowerShell 7 does.
	PowerShell
	// Unknown reads a line as Bash does but trusts none of it as data.
	Unknown
)

// Command is one simple command a line may run, at any nesting depth.
type Command struct {
	// Words are the command's words after quote removal and escapes.
	Words []string
	// Dirs are the directories it may run in; Anywhere among them means
	// one the hook cannot know.
	Dirs []string
	// Writes are the paths it may write, as typed (relative to a Dirs
	// entry): redirect targets, a write verb's operands, and files an
	// output option names.
	Writes []string
	// Whole are the paths it may rewrite or remove whole when they are
	// directories: the operands of rm, git checkout, Remove-Item, and the
	// like, in their own simple command
	// (knowledge/decisions/protected-path-enforcement.md §7).
	Whole []string
}

// Effects is what a command line may do.
type Effects struct {
	Commands []Command
	// Push says the line may push or open a pull request.
	Push bool
	// Code is the line with its data blanked (data.go), each byte in place;
	// the line itself unless it is all data.
	Code  string
	calls []call
}

// Invokes reports whether the line may run reliab with the command path
// cmd, such as "findings", "ack".
func (e *Effects) Invokes(cmd ...string) bool {
	for _, c := range e.calls {
		if commandPath(c.args, c.pwsh, cmd) {
			return true
		}
	}
	return false
}

// Read returns what text may do when sh runs it in dir. A line in an
// Unknown shell may do what Bash or PowerShell would. A lone carriage
// return is plain text to Bash but a line break to other readers, so where
// the line may write is also read with it as one; whether it invokes reliab
// is not (TestPreBashPersonOnlyInData pins that).
func Read(text string, sh Shell, dir string) *Effects {
	e := read(text, sh, dir)
	if sh == Unknown {
		p := read(text, PowerShell, dir)
		e.Commands = append(e.Commands, p.Commands...)
		e.calls = append(e.calls, p.calls...)
		e.Push = e.Push || p.Push
	}
	if sh != PowerShell && strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\r") {
		cr := read(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), sh, dir)
		e.Commands = append(e.Commands, cr.Commands...)
	}
	return e
}

func read(text string, sh Shell, dir string) *Effects {
	r := &reader{e: &Effects{}}
	l := lex(text, sh, 0)
	data := dataLine(l)
	r.e.Code = text
	if data {
		r.e.Code = codeText(l)
	}
	r.read(l, []string{dir}, data, 0)
	return r.e
}

// nestedCode is text a command runs as code, in its shell.
type nestedCode struct {
	text string
	sh   Shell
}

// reader collects a line's effects.
type reader struct {
	e       *Effects
	entered map[string]entered
	// scanned holds, per text maybeText read, the least depth it was read
	// at: a quoted body is met again in each word and line holding it, and
	// reading it once keeps a long one linear.
	scanned map[scanKey]int
}

type scanKey struct {
	text string
	sh   Shell
}

// read collects the effects of a lexed line whose commands start in dirs.
// data says the line is all data, so its data commands' arguments are
// never code.
func (r *reader) read(l *line, dirs []string, data bool, depth int) {
	r.place(l, dirs)
	pwsh := l.sh == PowerShell
	scripts := scriptsWritten(l)
	for k, c := range l.cmds {
		ws := c.texts()
		writes, whole, push, code, execs := effectsOf(ws, l.sh)
		writes = append(append([]string{}, c.outs...), writes...)
		if text, ok := runsScript(ws, scripts); ok {
			code = append(code, nestedCode{text, l.sh})
		}
		code = append(code, sourcedSubs(c, ws, l.sh)...)
		cmdDirs := union(c.cwds, c.may)
		writes, whole = expand(writes, cmdDirs, l.sh), expand(whole, cmdDirs, l.sh)
		if len(ws) > 0 || len(writes) > 0 {
			r.e.Commands = append(r.e.Commands, Command{Words: ws, Dirs: cmdDirs, Writes: writes, Whole: whole})
		}
		r.e.Push = r.e.Push || push
		upTo := len(ws)
		if k := dataArgs(c); data && k >= 0 {
			upTo = k
		}
		r.e.calls = append(r.e.calls, calls(c.words, pwsh, upTo)...)

		// Code the command runs: substitutions, subshells and groups,
		// a -c string, code its standard input carries.
		// A substitution on a line that is all data is all data itself.
		for _, w := range c.words {
			for _, s := range w.subs {
				r.read(s, cmdDirs, data, depth+1)
			}
		}
		for _, g := range c.groups {
			r.read(g, cmdDirs, false, depth+1)
		}
		for _, b := range c.bodies {
			for _, s := range b.subs {
				r.read(s, cmdDirs, data, depth+1)
			}
		}
		if depth < maxDepth {
			for _, nc := range code {
				r.read(lex(nc.text, nc.sh, depth+1), cmdDirs, false, depth+1)
			}
			for _, x := range execs {
				r.readWords(x, l.sh, cmdDirs, depth+1)
			}
		}
		stdinCode := readsCode(ws) || k+1 < len(l.cmds) && sepKind(c.sep) == "|" && readsCode(l.cmds[k+1].texts())
		if stdinCode && depth < maxDepth {
			sh := l.sh
			if h := head(ws); readsCode(ws) && h >= 0 && pwshes[name(ws[h])] {
				sh = PowerShell
			}
			for _, b := range c.bodies {
				r.read(lex(b.text, sh, depth+1), cmdDirs, false, depth+1)
			}
			if h := head(ws); !readsCode(ws) && h >= 0 && (name(ws[h]) == "echo" || name(ws[h]) == "printf") {
				r.read(lex(strings.Join(ws[h+1:], " "), sh, depth+1), cmdDirs, false, depth+1)
			}
		}
		if !data || dataArgs(c) < 0 {
			r.maybe(c, l.sh, depth)
		}
	}
	if !data {
		for _, t := range l.comments {
			r.maybeText(t, l.sh, depth)
		}
	}
}

// scriptsWritten returns the text a line's commands write to files, by
// file: a heredoc or here-string redirected to one, or echo's or printf's
// words. A shell may run such a file later on the line.
func scriptsWritten(l *line) map[string]string {
	var m map[string]string
	for _, c := range l.cmds {
		if len(c.outs) == 0 {
			continue
		}
		var texts []string
		for _, b := range c.bodies {
			texts = append(texts, b.text)
		}
		ws := c.texts()
		if h := head(ws); len(texts) == 0 && h >= 0 && (name(ws[h]) == "echo" || name(ws[h]) == "printf") {
			texts = append(texts, strings.Join(ws[h+1:], " "))
		}
		if len(texts) == 0 {
			continue
		}
		if m == nil {
			m = map[string]string{}
		}
		for _, out := range c.outs {
			m[filepath.Clean(filepath.FromSlash(out))] = strings.Join(texts, "\n")
		}
	}
	return m
}

// sourcedSubs returns the text a process substitution prints when a shell
// runs it as a script, as in . <(echo …) or bash <(printf …): the echo or
// printf words, or a heredoc body, of each command in it.
func sourcedSubs(c *simple, ws []string, sh Shell) []nestedCode {
	h := head(ws)
	if h < 0 {
		return nil
	}
	if n := name(ws[h]); !shells[n] && !pwshes[n] && n != "source" && n != "." {
		return nil
	}
	var out []nestedCode
	for _, w := range c.words[h+1:] {
		if !strings.HasPrefix(w.text, "<(") {
			continue
		}
		for _, s := range w.subs {
			for _, sc := range s.cmds {
				for _, b := range sc.bodies {
					out = append(out, nestedCode{b.text, sh})
				}
				sws := sc.texts()
				if k := head(sws); k >= 0 && (name(sws[k]) == "echo" || name(sws[k]) == "printf") {
					out = append(out, nestedCode{strings.Join(sws[k+1:], " "), sh})
				}
			}
		}
	}
	return out
}

// runsScript returns the text written earlier on the line to the file a
// command runs: as a shell's script (bash r.sh), by source or ., or as
// the command itself (./r.sh).
func runsScript(ws []string, scripts map[string]string) (string, bool) {
	h := head(ws)
	if h < 0 || len(scripts) == 0 {
		return "", false
	}
	file := ws[h]
	if n := name(ws[h]); shells[n] || pwshes[n] || n == "source" || n == "." {
		file = ""
		for _, a := range ws[h+1:] {
			if !strings.HasPrefix(a, "-") {
				file = a
				break
			}
		}
	}
	text, ok := scripts[filepath.Clean(filepath.FromSlash(file))]
	return text, ok && file != ""
}

// readWords collects the effects of a command given as words, as find
// -exec gives one.
func (r *reader) readWords(ws []string, sh Shell, dirs []string, depth int) {
	c := &simple{}
	for _, w := range ws {
		c.words = append(c.words, word{text: w})
	}
	r.read(&line{sh: sh, cmds: []*simple{c}}, dirs, false, depth)
}

// maybe reads, for reliab invocations only, text in a command that a shell
// may yet run: a word holding spaces (git rebase --exec "…", a script
// written to a file), a heredoc body or here-string. A word's spaces mean
// it lost its quotes, so it is shorter than its line: this ends.
func (r *reader) maybe(c *simple, sh Shell, depth int) {
	for _, w := range c.words {
		if strings.ContainsAny(w.text, " \t\n") {
			r.maybeText(w.text, sh, depth)
		}
	}
	for _, b := range c.bodies {
		r.maybeText(b.text, sh, depth)
	}
}

// maybeText reads text for the reliab invocations it may hold. Text with
// no reliab, variable, splat, or substitution in it holds none.
func (r *reader) maybeText(t string, sh Shell, depth int) {
	if depth >= maxDepth || !strings.Contains(strings.ToLower(t), "reliab") && !strings.ContainsAny(t, "$@`(") {
		return
	}
	k := scanKey{t, sh}
	if d, ok := r.scanned[k]; ok && d <= depth {
		return
	}
	if r.scanned == nil {
		r.scanned = map[scanKey]int{}
	}
	r.scanned[k] = depth
	l := lex(t, sh, depth+1)
	pwsh := sh == PowerShell
	l.any(func(c *simple) bool {
		r.e.calls = append(r.e.calls, calls(c.words, pwsh, len(c.words))...)
		r.maybe(c, sh, depth+1)
		return false
	})
	for _, t := range l.comments {
		r.maybeText(t, sh, depth+1)
	}
}

// texts returns a command's words' texts.
func (c *simple) texts() []string {
	out := make([]string, len(c.words))
	for i, w := range c.words {
		out[i] = w.text
	}
	return out
}

// any reports whether f holds for a command of l or of code nested in one.
func (l *line) any(f func(*simple) bool) bool {
	for _, c := range l.cmds {
		if f(c) {
			return true
		}
		for _, w := range c.words {
			for _, s := range w.subs {
				if s.any(f) {
					return true
				}
			}
		}
		for _, g := range c.groups {
			if g.any(f) {
				return true
			}
		}
	}
	return false
}
