#!/bin/sh
# PreToolUse hook for Bash and PowerShell in reliab's own repo. It warns,
# never blocks, when a command writes a .go or .md file through python,
# perl, sed -i, a redirect or heredoc, tee, or PowerShell's content
# cmdlets: those bypass the editor tools, and have left CRLF, control bytes,
# and mangled encoding in files here (knowledge/decisions/dev-environment.md).
# It runs on every shell call, so it uses shell builtins, and one grep only
# when a cheap check already matched. It always exits 0.

input=
while IFS= read -r line || [ -n "$line" ]; do
  input="$input$line "
done

# The command string, still JSON-escaped. Cutting at the first "," or "}
# can only shorten it, which may miss a write but never invents one.
case $input in *'"command"'*) ;; *) exit 0 ;; esac
cmd=${input#*\"command\"}
cmd=${cmd#*\"}
cmd=${cmd%%\",\"*}
cmd=${cmd%%\"\}*}

case $cmd in *.go*|*.md*|*.GO*|*.MD*) ;; *) exit 0 ;; esac

# Text a commit, tag, PR, or issue records is data, not a write.
cmd=${cmd%%git commit*}
cmd=${cmd%%git tag*}
cmd=${cmd%%gh pr *}
cmd=${cmd%%gh issue *}

how=
case $cmd in
  *python*|*'py -'*|*py.exe*)
    case $cmd in
      *write*|*"'w'"*|*'\"w\"'*|*"'a'"*|*'\"a\"'*|*"'wb'"*|*'\"wb\"'*|*"'w+'"*|*'\"w+\"'*) how=python ;;
    esac ;;
esac

if [ -z "$how" ]; then
  # A heredoc's body is data: keep its first line, where the redirect is.
  case $cmd in
    *'<<'*)
      rest=${cmd#*<<}
      cmd="${cmd%%<<*}<<${rest%%\\n*}" ;;
  esac
  case $cmd in
    *'>'*|*sed*|*perl*|*tee*|*ontent*|*ut-[Ff]ile*|*WriteAll*)
      f="(\\\\\"|')?[^][:space:]&|;<>\"'\`()]*\\.(go|md)([^[:alnum:]_.]|\$)"
      re="(^|[^>=-])>>?[[:space:]]*$f"
      re="$re|(^|[^[:alnum:]_-])(sed|perl)[[:space:]][^|;&]*(-[a-z]*i|--in-place)[^|;&]*\\.(go|md)([^[:alnum:]_.]|\$)"
      re="$re|(^|[^[:alnum:]_-])tee[[:space:]]+(-a[[:space:]]+)?$f"
      re="$re|(set-content|add-content|out-file|writeall(text|lines|bytes))[^|;]*\\.(go|md)([^[:alnum:]_.]|\$)"
      if printf '%s\n' "$cmd" | grep -Eiq -- "$re"; then
        how="a shell write"
      fi ;;
  esac
fi

[ -n "$how" ] || exit 0
printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"%s"}}\n' \
  "reliab dev: this command writes a .go or .md file through $how. Shell writes have left CRLF, control bytes, and mangled encoding in this repo's files; prefer the Edit or Write tool, or check the file afterwards (git diff, gofmt -l). This is a warning; the command was not blocked."
exit 0
