# PreToolUse hook for Bash and PowerShell in reliab's own repo. It warns,
# never blocks, when a command writes a .go or .md file through python,
# perl -i, sed -i, a redirect, tee, or PowerShell's content cmdlets: those
# bypass the editor tools, and have left CRLF, control bytes, and mangled
# encoding in files here (knowledge/decisions/dev-environment.md).
#
# It runs on every shell call, so it is one awk process with no other
# spawn, and its work is linear in the command's length. It splits the
# command the way a shell would (quotes, heredocs, comments, ; && || |) and
# looks only at the commands that can write a file, so a path, a commit
# message, or a heredoc's data never reads as a write. It always exits 0.

{ input = input $0 "\n" }

END {
  low = tolower(input)
  if (index(low, ".go") == 0 && index(low, ".md") == 0) exit 0
  if (!match(input, /"command"[ \t\r\n]*:[ \t\r\n]*"/)) exit 0
  rest = substr(input, RSTART + RLENGTH)
  # The JSON string runs to its first unescaped quote.
  if (!match(rest, /^([^"\\]|\\.)*/)) exit 0
  cmd = unjson(substr(rest, 1, RLENGTH))
  low = tolower(cmd)
  if (low !~ /\.(go|md)/) exit 0
  if (low !~ />|sed|perl|tee|py|content|out-file|writeall|appendall/) exit 0
  ps = input ~ /"tool_name"[ \t\r\n]*:[ \t\r\n]*"PowerShell"/
  how = lex(cmd, ps)
  if (how == "") exit 0
  printf "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"additionalContext\":\"%s\"}}\n", \
    "reliab dev: this command writes a .go or .md file through " how ". Shell writes have left CRLF, control bytes, and mangled encoding in this repo's files; prefer the Edit or Write tool, or check the file afterwards (git diff, gofmt -l). This is a warning; the command was not blocked."
  exit 0
}

# unjson decodes the escapes a command can hold; \uXXXX stays as written,
# which cannot turn into a .go or .md write. It splits at backslashes once
# instead of running gsub, whose cost grows with the square of a long
# heredoc's line count.
function unjson(s,    n, p, i, out, e) {
  n = split(s, p, "\\")
  out = p[1]
  for (i = 2; i <= n; i++) {
    if (p[i] == "") { out = out "\\" p[++i]; continue }
    e = substr(p[i], 1, 1)
    out = out (e == "n" ? "\n" : e == "t" ? "\t" : e == "r" ? "\r" : e == "u" ? "\\u" : e) substr(p[i], 2)
  }
  return out
}

# isgomd is a path ending in .go or .md; gomd also rules out an argument
# with spaces, which is text more often than a path.
function isgomd(w) { return tolower(w) ~ /\.(go|md)$/ }
function gomd(w) { return isgomd(w) && w !~ /[ \t\n]/ }

# lex splits the command into simple commands (words, redirect targets,
# heredoc bodies) and returns how the first write to a .go or .md file
# happens, or "". Its state is global so endword can update it.
function lex(s, ps,    n, c, i, k, rest, re, how) {
  n = split(s, c, "")
  nseg = 1; nw = 0; word = ""; inword = 0; pend = ""; q = ""; nh = 0
  for (i = 1; i <= n + 1; i++) {
    ch = (i <= n) ? c[i] : "\n"
    if (q == "'") {
      if (ch == "'") q = ""; else word = word ch
      continue
    }
    if (q == "\"") {
      if (ch == "\"") q = ""
      else if (!ps && ch == "\\" && i < n && c[i+1] ~ /["\\$`]/) word = word c[++i]
      else if (ps && ch == "`" && i < n) word = word c[++i]
      else word = word ch
      continue
    }
    if (ch == "'" || ch == "\"") { q = ch; inword = 1; continue }
    if ((!ps && ch == "\\" || ps && ch == "`") && i < n) {
      if (c[i+1] != "\n") { word = word c[i+1]; inword = 1 }
      i++; continue
    }
    if (ch == "#" && !inword) {
      while (i < n && c[i+1] != "\n") i++
      continue
    }
    if (ch == ">" || ch == "<" || (ch == "&" && i < n && c[i+1] == ">")) {
      # A redirect: a word of digits right before it is its descriptor.
      if (inword && pend == "" && word ~ /^[0-9]+$/) { word = ""; inword = 0 }
      else if ((how = endword()) != "") return how
      if (ch == "&") { i++; ch = ">" }
      if (ch == ">") {
        if (i < n && c[i+1] == ">") i++
        if (i < n && c[i+1] == "|") i++
        if (i < n && c[i+1] == "&") { i++; pend = "dup" } else pend = "out"
      } else if (i + 2 <= n && c[i+1] == "<" && c[i+2] == "<") { i += 2; pend = "in" }
      else if (!ps && i < n && c[i+1] == "<") {
        i++; pend = "heredoc"
        if (i < n && c[i+1] == "-") { i++; pend = "heredoc-" }
      } else pend = "in"
      continue
    }
    if (ch == " " || ch == "\t" || ch == "\r" || ch == "\n" || ch == ";" || ch == "|" || ch == "&" || \
        (!ps && (ch == "(" || ch == ")")) || (ps && (ch == "{" || ch == "}"))) {
      if ((how = endword()) != "") return how
      if (ch == " " || ch == "\t" || ch == "\r") continue
      if ((how = endseg(nseg, nw)) != "") return how
      nseg++; nw = 0; pend = ""
      if (ch != "\n" || nh == 0) continue
      # Heredoc bodies follow this line, one per delimiter, in order. One
      # regex match finds each body's end, so a long body costs one pass.
      for (k = 1; k <= nh; k++) {
        rest = "\n" substr(s, i + 1)
        re = hdel[k]
        gsub(/[^A-Za-z0-9_]/, "\\\\&", re)
        re = "\n" (hstrip[k] ? "\t*" : "") re "\r?(\n|$)"
        if (match(rest, re)) {
          segbody[hseg[k]] = segbody[hseg[k]] substr(rest, 2, RSTART - 1)
          i += RSTART + RLENGTH - 2
        } else {
          segbody[hseg[k]] = segbody[hseg[k]] substr(rest, 2)
          i = n
        }
      }
      nh = 0
      for (k = 1; k < nseg; k++) if (!(k in done) && (how = pycheck(k)) != "") return how
      continue
    }
    word = word ch; inword = 1
  }
  for (k = 1; k <= nseg; k++) if (!(k in done) && (how = pycheck(k)) != "") return how
  return ""
}

# endword finishes the current word: a heredoc delimiter, a redirect
# target, or the segment's next word.
function endword(    w, p) {
  if (!inword) return ""
  w = word; p = pend
  word = ""; inword = 0; pend = ""
  if (p ~ /^heredoc/) { hdel[++nh] = w; hseg[nh] = nseg; hstrip[nh] = (p == "heredoc-"); return "" }
  if (p == "out") return isgomd(w) ? "a shell redirect" : ""
  if (p == "dup") return (w !~ /^([0-9]+|-)$/ && isgomd(w)) ? "a shell redirect" : ""
  if (p == "in") return ""
  words[nseg, ++nw] = w
  return ""
}

# cmdstart returns the index of the segment's command word, past variable
# assignments, keywords, and wrappers that run the next word.
function cmdstart(seg,    k, w) {
  for (k = 1; k <= nwords[seg]; k++) {
    w = words[seg, k]
    if (w ~ /^[A-Za-z_][A-Za-z0-9_]*=/) continue
    if (w ~ /^(!|\{|\}|then|do|else|elif|if|while|until|time|exec|command|sudo|env|nohup)$/) continue
    return k
  }
  return 0
}

function base(w) { w = tolower(w); sub(/^.*[\/\\]/, "", w); sub(/\.exe$/, "", w); return w }

# endseg checks the commands that write through their arguments.
function endseg(seg, nw,    k, s, name, w, i, opts, j) {
  nwords[seg] = nw
  if (nw == 0) return ""
  s = cmdstart(seg)
  if (s == 0) return ""
  name = base(words[seg, s])
  if (name == "sed" || name == "perl") {
    opts = (name == "sed") ? "^-[nrEsuzl0-9]*i" : "^-[0-9pnlawsTtuUWX]*i"
    for (i = s + 1; i <= nw; i++) {
      w = words[seg, i]
      if (w ~ opts || w ~ /^--in-place/) {
        for (j = s + 1; j <= nw; j++) if (words[seg, j] !~ /^-/ && gomd(words[seg, j])) return name " -i"
        return ""
      }
    }
    return ""
  }
  if (name == "tee" || name == "tee-object" || name == "set-content" || name == "add-content" || \
      name == "out-file" || (ps && (name == "sc" || name == "ac"))) {
    for (i = s + 1; i <= nw; i++) if (words[seg, i] !~ /^-/ && gomd(words[seg, i])) return name
    return ""
  }
  if (ps && tolower(words[seg, s]) ~ /(writeall(text|lines|bytes)|appendall(text|lines))\(/) {
    for (i = s; i <= nw; i++) if (tolower(words[seg, i]) ~ /\.(go|md)([^a-z0-9_.]|$)/) return "a .NET file write"
  }
  return ""
}

# pycheck runs once a segment's heredoc bodies are known: python's code is
# its -c argument or, read from stdin, its heredoc body.
function pycheck(seg,    s, name, i, code, script) {
  done[seg] = 1
  s = cmdstart(seg)
  if (s == 0) return ""
  name = base(words[seg, s])
  if (name !~ /^(python[0-9.]*|py)$/) return ""
  code = ""; script = 0
  for (i = s + 1; i <= nwords[seg]; i++) {
    if (words[seg, i] == "-c") { code = words[seg, i+1]; break }
    if (words[seg, i] == "-") break
    if (words[seg, i] !~ /^-/) { script = 1; break }
  }
  if (code == "" && !script) code = segbody[seg]
  return pywrites(code) ? "python" : ""
}

# pywrites reports whether python code writes a .go or .md file: an open
# for writing, or a pathlib write, whose path is a .go or .md literal, or a
# name when the code holds such a literal.
function pywrites(code,    low, lit, r, arg) {
  low = tolower(code)
  lit = low ~ /["'][^"'\n]*\.(go|md)["']/
  r = low
  while (match(r, /open\([^()]*,[ ]*(mode[ ]*=[ ]*)?["'][rwxabt]*[wax+][rwxabt+]*["']/)) {
    arg = substr(r, RSTART + 5, RLENGTH - 5)
    r = substr(r, RSTART + RLENGTH)
    if (arg ~ /^[ ]*["']/) { if (arg ~ /^[ ]*["'][^"']*\.(go|md)["']/) return 1 }
    else if (lit) return 1
  }
  if (!lit) return 0
  r = low
  while (match(r, /\.(write_text|write_bytes)\(|\.open\([ ]*(mode[ ]*=[ ]*)?["'][rwxabt]*[wax+]/)) {
    arg = substr(r, 1, RSTART - 1)
    r = substr(r, RSTART + RLENGTH)
    if (arg ~ /\([ ]*["'][^"']*["'][ ]*\)$/ && arg !~ /\.(go|md)["'][ ]*\)$/) continue
    return 1
  }
  return 0
}
