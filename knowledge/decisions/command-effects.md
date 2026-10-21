---
type: Decision
title: "The pre-bash hook reads a command line once and judges its effects"
description: "internal/guard/effects reads a Bash or PowerShell line once (continuations, quotes and escapes per shell, curly quotes, heredocs, substitutions, code nested in -c, -Command, eval, iex) and returns its simple commands with their words, the directories each may run in, the paths each may write or remove whole, the reliab command paths it may invoke, and whether it pushes. PreBash only judges those effects. Data stays data: quoted text is never a redirect or a verb, a heredoc body is data unless a shell reads it, and push is a git push a shell runs."
status: draft
tags: [hooks, enforcement, protected-paths, performance]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-20T19:30:00-07:00
---

# The pre-bash hook reads a command line once and judges its effects

## Context

Until 2026-10-20 the pre-bash hook read one line six ways: `commandSep` and
`units` split it for the directory model, `segments` split it again for the
data rule, `words` read quotes, the `writeVerb`, `redirect`, and `fileDup`
regexes and a `tokenSep` split read the raw text for writes, and
`personOnly` normalized it its own way for the person-only commands. A
respelling a review taught one reader stayed open in the others. An
architecture review of v0.1.5 denied `reliab \⏎ findings ack`, `bash -c
'reliab findings ack F1'`, and `reli\ab findings ack`, and allowed the same
spellings of a protected write: `cp x \⏎ snapshots/s.txt`, `bash -c 'cp x
snapshots/s.txt'`, `cp x sn\apshots/s.txt`, PowerShell's ``Copy-Item x `⏎
snapshots/s.txt`` and curly quotes. `rm \⏎ snapshots/s.txt` was denied only
because the stray backslash read as the repository root, which also denied
`rm \⏎ src/a.txt`.

The raw-text readers also denied legitimate commands, which costs as much
as a bypass: the retro of 2026-10-20 found `cd <worktree> && git commit -m
"… reliab approve …"`, `git -C <dir> commit -m …`, and the
`git commit -m "$(cat <<'EOF' … EOF)"` form denied as running
`reliab approve`; "git push" inside test JSON, a commit message, or `git
log --grep push` sent to the push gate; a sample repository a line makes
with `mkdir -p … && cd …` judged as this repository; and a heredoc body
that names a write (`cat > notes.md <<'EOF'` … `rm snapshots/s.txt`)
denied. It also found `rm -rf \⏎ .reliab/characterize` allowed in a
repository with no tracked oracle, and `find .reliab/characterize -delete`
allowed.

## Decision

1. **One reader.** `effects.Read(line, shell, dir)` reads a line once, the
   way its shell does: Bash's quotes, escapes, `$'…'`, and continuations
   (GNU Bash manual §3.1.2); PowerShell's quotes (curly ones too, doubled
   quotes inside), backtick escapes and continuations, no-break and other
   Unicode spaces, dashes before a parameter, here-strings, and block
   comments; separators and redirections (`>`, `>>`, `>|`, `n>`, `&>`,
   `>&file`, `*>`), with a redirect's `&` never splitting a command;
   heredocs, whose body ends at its first exact closing line (§3.6.6), so
   what follows is read as commands; comments; and command and process
   substitution, subshells, and PowerShell's groups and script blocks, read
   as code of their own. Code a command runs is read as a line of its own:
   the string after `sh`/`bash` `-c` (any option cluster holding `c`),
   `pwsh -Command`, `cmd /c`, `eval`, `iex`, `watch`, `git rebase --exec`,
   a git alias starting `!` and `core.pager`/`core.editor`, the commands
   `find -exec` runs, and a heredoc, here-string, or `echo` text a shell
   reads from standard input. A line from an unknown tool is read both as
   Bash and as PowerShell. It returns:
   - **commands**, each with its words after quote removal;
   - the **directories** each may run in: the cwd model as it was
     ([hook-decisions-v0](/decisions/hook-decisions-v0.md)), with
     `Dirs[0]` where it runs as far as the hook can follow;
   - the **paths each may write**: redirect targets, a write verb's
     operands, and output options;
   - the **paths each may remove whole**
     ([protected-path-enforcement](/decisions/protected-path-enforcement.md) §7);
   - the **reliab command paths** the line may invoke (`Invokes`), found as
     cobra finds them;
   - whether it **pushes**: `git … push` past git's global options, or
     `gh pr create`, where a shell runs them;
   - **Code**, the line with its data blanked, each byte in place.
   `PreBash` only judges effects: invocations against the agent type,
   writes and whole removals against the protected set, a push against the
   [pre-push gate](/decisions/pre-push-v0.md). `protected_commands` stay a
   raw-text check (protected-path-enforcement §5). The four person-only
   regexes stay too, run on Code: they also match reliab's words glued to
   other text and in any case (`Reliab.EXE Findings Ack`, `reliab.cmd
   APPROVE`), which cobra's case-sensitive matching does not, and no
   property shows the reader covers every line they catch.
2. **Data is not absent.** Quoted text is a word, never a redirect or a
   verb, so `git commit -m "fix > snapshots/s.txt; rm x"` writes nothing. A
   write verb counts only as the command a simple command runs, past
   assignments, keywords, and wrappers with their options (`sudo`, `env`,
   `xargs`, `timeout`, `nice`, `nohup`, `exec`, `command`, PowerShell's
   `&`), by base name (`/bin/rm` is `rm`); a write verb's name as data
   (`grep -rn cp snapshots`, `echo rm x`, `go install`) writes nothing. A
   heredoc body is data unless a shell reads it. Text a shell may yet run
   later (a quoted word holding spaces in a command that is not data, a
   comment, a heredoc body fed to another program) is read for reliab
   invocations only, never for writes or pushes, as `personOnly` read it.
3. **The data rule** is unchanged in kind: a Bash line has its data
   blanked only when every simple command on it is a data command and none
   is doubtful. Data commands now also include `cd` and `pushd`, git with
   `-C`, `--git-dir`, `--work-tree`, `--namespace`, `--no-pager`, or `-P`
   before a recording subcommand (never `-c`), and a command substitution
   whose own code is all data, as in `git commit -m "$(cat <<'EOF' …
   EOF)"`. PowerShell and unknown tools are never data.
4. **Doubt is kept.** The cwd model's `trackable` still reads the raw line
   (continuations joined), so a heredoc, a comment, a quote holding a
   separator, a substitution, or non-ASCII text still judges from the
   session's directory, and a `cd` with a redirection stays unfollowed. One
   addition: a `mkdir` of an absolute path earlier in the same `&&` chain
   makes a `cd` there followable, as named, though the directory does not
   exist when the hook runs. A lone carriage return is text to Bash, but
   the line is also read with it as a line break for where it may write.
5. **New writes.** `find` with `-delete`, or `-exec` running a write verb,
   writes and removes whole the paths it starts from; `dd of=`; a
   PowerShell `-Name:value` parameter of a write verb.
6. **One edit-time protected set.** `profile.EditSet` holds reliab's
   outputs (`.reliab/**`) and the profile's protected paths, minus the
   profile while HEAD has no copy of it, and asks git only when the
   profile's own path is judged. The hooks and `profile init`'s deny rules
   use it ([protected-path-approval](/decisions/protected-path-approval.md));
   `DenyRules` moved from doctor to profile. `reliab gate protected` keeps
   its own matching of git's diff.
7. **Cost.** The hook runs on every Bash call, so the reader is one pass
   with no regex compiled per call; a `cd`'s stat and symlink resolution
   are kept for the rest of the read; a write target's physical path
   resolves each run of components between `..`s in one call, and once per
   hook call; the repository's file list is still read only when a command
   may run anywhere or removes a directory whole; the person-only regexes
   run only on a line that names reliab, and the `cd` regex only on one
   holding a `cd`-like word. On the benchmark in
   `internal/guard/bench_test.go` (Windows, Ryzen 7 9800X3D), PreBash went
   from 9.7 to 3.0 µs for `go test ./...`, 650 to 22 µs for a commit with a
   1,200-byte message, 40 to 6.6 µs for a pipeline, 2.1 to 0.69 ms for a
   `cd` chain, 2.5 to 0.56 ms for a redirect into the repository, and 37 to
   7.5 µs for a PowerShell pipeline. What remains on the last two is the
   file system: a `cd` and a write target are resolved through symlinks,
   which also expands Windows short names.

## Consequences

A respelling learnt once holds for every check: a property test checks that
continuations, quoting, escaped letters, and wrapping in `bash -c` or
`pwsh -Command` leave a line's writes, removals, pushes, and invocations as
the plain spelling has them. The hook remains a heuristic: a function or
alias, a script, another language's process call, and a heredoc after a
`cd` (still untrackable, so judged from the session's directory) are
beyond it, and CI's `reliab gate protected` stays authoritative
([enforcement-hooks-self-contained](/decisions/enforcement-hooks-self-contained.md)).
