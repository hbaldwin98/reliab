package effects

import (
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// knowledge/decisions/command-effects.md: the shell undoes a respelling
// before it runs a word, so a respelled line does what its plain spelling
// does, for writes and reliab invocations alike. GNU Bash manual §3.1.2.1
// (a backslash before a newline continues the line), §3.1.2.2 and §3.1.2.3
// (single and double quotes), §3.1.2 (a backslash escapes the next
// character), and bash(1) -c (the string is read as commands); PowerShell
// about_Quoting_Rules and about_Special_Characters (a backtick continues a
// line and escapes the next character), and pwsh -Command. An architecture
// review of v0.1.5 found writes respelled each way that the guard allowed
// while it denied the same respelling of reliab findings ack.

// token is a word of a template line, or an operator (a redirect or a
// separator) that a respelling leaves alone.
type token struct {
	text string
	op   bool
}

func w(s string) token  { return token{text: s} }
func op(s string) token { return token{text: s, op: true} }

var dests = []string{"snapshots/s.txt", ".reliab/x.json", "src/a.txt", "out/file-1.txt"}

// templates are typical lines: writes of every kind the guard judges,
// reliab's person-only commands, pushes, and harmless commands. %s is a
// destination.
func templates(pwsh bool) [][]token {
	if pwsh {
		return [][]token{
			{w("Copy-Item"), w("x"), w("%s")},
			{w("Remove-Item"), w("-Recurse"), w("%s")},
			{w("Set-Content"), w("%s"), w("x")},
			{w("del"), w("%s")},
			{w("echo"), w("x"), op(">"), w("%s")},
			{w("Invoke-WebRequest"), w("https://example.com"), w("-OutFile"), w("%s")},
			{w("reliab"), w("findings"), w("ack"), w("F-1")},
			{w("reliab"), w("approve"), w("%s")},
			{w("reliab"), w("report"), w("verdict")},
			{w("git"), w("push"), w("origin"), w("main")},
			{w("go"), w("test"), w("./...")},
		}
	}
	return [][]token{
		{w("cp"), w("x"), w("%s")},
		{w("mv"), w("a"), w("%s")},
		{w("rm"), w("-rf"), w("%s")},
		{w("tee"), w("%s")},
		{w("echo"), w("x"), op(">"), w("%s")},
		{w("echo"), w("x"), op(">>"), w("%s")},
		{w("sed"), w("-i"), w("s/a/b/"), w("%s")},
		{w("git"), w("checkout"), w("--"), w("%s")},
		{w("git"), w("-C"), w("."), w("restore"), w("%s")},
		{w("curl"), w("-o"), w("%s"), w("https://example.com")},
		{w("find"), w("%s"), w("-delete")},
		{w("reliab"), w("findings"), w("ack"), w("F-1")},
		{w("reliab"), w("--repo"), w("."), w("approve"), w("%s")},
		{w("reliab"), w("report"), w("verdict"), w("--outcome"), w("pass")},
		{w("reliab"), w("baseline"), w("write")},
		{w("git"), w("push"), w("origin"), w("main")},
		{w("gh"), w("pr"), w("create")},
		{w("go"), w("test"), w("./...")},
	}
}

// signature is what a line may do, in any order.
func signature(e *Effects) []string {
	var out []string
	for _, c := range e.Commands {
		for _, x := range c.Writes {
			out = append(out, "write "+x)
		}
		for _, x := range c.Whole {
			out = append(out, "whole "+x)
		}
	}
	if e.Push {
		out = append(out, "push")
	}
	for _, p := range [][]string{{"findings", "ack"}, {"approve"}, {"baseline", "write"}, {"report", "verdict"}} {
		if e.Invokes(p...) {
			out = append(out, "reliab "+strings.Join(p, " "))
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// respellWord returns a spelling of a word that its shell reads as the
// word: as is, quoted whole, with escaped letters, or (Bash) quoted in
// part.
func respellWord(t *rapid.T, word string, pwsh bool) string {
	kind := rapid.IntRange(0, 3).Draw(t, "kind")
	if kind == 1 && !strings.Contains(word, "'") {
		return "'" + word + "'"
	}
	if kind == 2 {
		var b strings.Builder
		for _, r := range word {
			letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
			// A PowerShell backtick before these letters is an escape
			// sequence inside double quotes; keep clear of them.
			if letter && (!pwsh || !strings.ContainsRune("0abefnrtuvABEFNRTUV", r)) && rapid.Bool().Draw(t, "escape") {
				if pwsh {
					b.WriteByte('`')
				} else {
					b.WriteByte('\\')
				}
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	if kind == 3 && !pwsh && len(word) > 1 && !strings.ContainsAny(word, `"$`+"`\\") {
		cut := rapid.IntRange(1, len(word)-1).Draw(t, "cut")
		return word[:cut] + `"` + word[cut:] + `"`
	}
	return word
}

func TestRespellingKeepsEffects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rapid.Check(t, func(t *rapid.T) {
		pwsh := rapid.Bool().Draw(t, "pwsh")
		sh := bashOrPwsh(pwsh)
		ts := templates(pwsh)
		line := rapid.SliceOfN(rapid.IntRange(0, len(ts)-1), 1, 2).Draw(t, "templates")
		dest := rapid.SampledFrom(dests).Draw(t, "dest")
		var plain, spelled []string
		for k, i := range line {
			if k > 0 {
				sep := rapid.SampledFrom([]string{"&&", ";", "||"}).Draw(t, "sep")
				plain, spelled = append(plain, sep), append(spelled, sep)
			}
			for _, tok := range ts[i] {
				text := strings.ReplaceAll(tok.text, "%s", dest)
				plain = append(plain, text)
				if tok.op {
					spelled = append(spelled, text)
				} else {
					spelled = append(spelled, respellWord(t, text, pwsh))
				}
			}
		}
		cont := " \\\n "
		if pwsh {
			cont = " `\n "
		}
		var b strings.Builder
		for k, s := range spelled {
			if k > 0 {
				if rapid.IntRange(0, 3).Draw(t, "gap") == 0 {
					b.WriteString(cont)
				} else {
					b.WriteByte(' ')
				}
			}
			b.WriteString(s)
		}
		respelled := b.String()
		if rapid.IntRange(0, 3).Draw(t, "wrap") == 0 {
			if !pwsh && !strings.Contains(respelled, "'") {
				respelled = "bash -c '" + respelled + "'"
			} else if pwsh && !strings.ContainsAny(respelled, `"$`+"`") {
				respelled = `pwsh -Command "` + respelled + `"`
			}
		}
		want := signature(Read(strings.Join(plain, " "), sh, dir))
		got := signature(Read(respelled, sh, dir))
		if !slices.Equal(got, want) {
			t.Fatalf("%q does %q, but its plain spelling %q does %q", respelled, got, strings.Join(plain, " "), want)
		}
	})
}

// knowledge/decisions/hook-decisions-v0.md: a hook that fails lets the
// command through, and a malformed line must still get a decision. So the
// reader ends, without a panic, on any text: unbalanced quotes and
// parentheses, an unterminated heredoc or substitution, stray escapes.
func TestReadEndsOnAnyLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pieces := []string{"a", " ", "\n", "\r", "\\", "`", "'", "\"", "$", "(", ")", "{", "}", "<", "<<", "<<-", "<<<", ">", ">&", "&", "|", ";", "#", "@'", "'@", "@\"", "\u201c", "\u2018", "\u00a0", "cd ", "rm ", "reliab ", "bash -c ", "EOF", "-", "~"}
	rapid.Check(t, func(t *rapid.T) {
		line := strings.Join(rapid.SliceOfN(rapid.SampledFrom(pieces), 0, 40).Draw(t, "line"), "")
		for _, sh := range []Shell{Bash, PowerShell, Unknown} {
			e := Read(line, sh, dir)
			_ = e.Invokes("findings", "ack")
		}
	})
}
