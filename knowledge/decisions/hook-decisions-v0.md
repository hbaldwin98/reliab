---
type: Decision
title: "What the v0 hooks decide"
description: "pre-edit, pre-bash, stop, and session-start rules; human-only acknowledgement; fail open on a broken profile; never exit 2 on a hook error."
status: stable
tags: [hooks, enforcement]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T18:05:00-07:00
---

# What the v0 hooks decide

## Decision

- **pre-edit** (Edit, Write, NotebookEdit): deny when the target is under a
  protected path or `.reliab/`. Directory targets count when they contain a
  protected path.
- **pre-bash** (Bash and PowerShell, one process per call; see
  [enforcement-hooks-self-contained](/decisions/enforcement-hooks-self-contained.md)):
  1. deny `reliab findings ack`, which records a human decision, and
     `reliab baseline write`, which changes the protected mutation baseline
     ([mutation-threshold](/decisions/mutation-threshold.md)), and
     `reliab approve`, a person's approval of a protected-path change
     ([protected-path-approval](/decisions/protected-path-approval.md)); deny
     `reliab report verdict` unless the caller's `agent_type` is
     `reliab:reviewer` ([reviewer-verdict](/decisions/reviewer-verdict.md)).
     They match however the line spells the command: each simple
     command's words are read past quotes and escapes (a PowerShell
     backtick too), any word whose base name is `reliab` (with `.exe`,
     `.cmd`, `.bat`, or `.ps1`, in any case) or a variable that may hold
     it starts a command, and its command path is found as cobra finds
     it, past global flags and the word after a flag with no `=`. Until
     2026-10-20 they matched the words as typed, so `reliab "findings"
     ack`, `relib findings ack`, `reliab --repo . findings ack`, and
     `$r findings ack` passed; an alias or function wrapping `reliab` still
     does. These match only text Bash may run. A Bash line whose every simple
     command is `echo`, `cat`, `grep`, or `git add|commit|tag|notes|status|log|show|diff`
     has its arguments and heredoc bodies blanked first, unless any command
     on it holds, outside quotes, a comment, `$'`, parentheses, braces, a
     redirect, or a backtick, or a substitution in its quotes or an
     expanded heredoc body. Any other line is judged whole, as is every
     PowerShell line. Until 2026-10-19 the words anywhere in the line were
     denied, so a commit whose message named `reliab approve` was refused;
     a review of the first, looser fix found nine bypasses (a comment
     hiding a quote or heredoc, a redirect writing a script, PowerShell's
     `( )` and curly quotes), now tests;
  2. deny commands that write, move, or delete a protected path or `.reliab/`:
     redirects (`>`, `>>`, numbered as in `2>`, `&>`, `>&file`, and `>|`;
     until 2026-10-20 a numbered or `&` redirect, `>&file`, and `>|` were
     missed) to such a path, a write verb (`sed -i`, `cp`,
     `mv`, `rm`, `tee`, `git checkout|restore`, `Set-Content`,
     `Remove-Item`, PowerShell's aliases for them such as `del`, `ri`,
     `copy`, and `sc`, and similar; until 2026-10-20 the aliases were
     missed) followed by one in the same simple command
     (the command line split at `;`, `&`, `|`, and newlines), or an option
     naming one for output (`--output`, `--output-file`,
     `--output-directory`, `-OutFile` or `-OutFile:<path>`, and `-o` for
     `git format-patch`, `curl`, `sort`, and `go`, `-O` for `wget`, the
     value attached or not; until 2026-10-20 `git diff --output=<path>`
     wrote any file; git's abbreviated long options such as `--outp` are
     not read). A Git Bash path on a drive (`/c/...`) is judged as that
     drive's path too. Heuristic and
     deliberately conservative: `cp snapshots/x /tmp` is denied too. Until
     2026-10-18 a write verb anywhere in the line made every path-like word
     a target, so an onboarding session had three reads denied, such as
     `ls .reliab/out/x` and `git diff -- '*.verified.*'` beside an unrelated
     `rm`. A relative path resolves where its simple command runs: a `cd`,
     `pushd`, or `Set-Location` to one literal directory that exists and
     can be entered moves the commands joined to it by `&&`, judged both at
     the directory as named and as resolved through symlinks (`env -C` and
     `--directory` add theirs). Never outside the repository unless the line
     surely runs it there: `;`, a newline, `||`, a pipe, or a background `&`
     ends what a `cd` moves; any doubt (a variable, `cd -`, `popd`, a loop,
     a subshell or brace group, a separator in quotes or after a backslash,
     a comment, a heredoc, `eval` or `source`, an alias or function, a
     PowerShell script, `CDPATH`, a non-ASCII character) judges from the
     session's directory. That keeps a write from moving out of the
     repository; a path is also judged everywhere its command may run,
     since a `cd` stays in effect past `;`, `||`, and a newline: in each
     directory any command before it in the chain ran in, and after a `cd`
     the hook cannot follow (or `popd`, `source`, or a line it cannot track
     with a directory change anywhere in it, even inside `(`, `$(`, `{`, or
     a backtick: a PowerShell group or script block keeps the location),
     from every directory holding a file git sees but an oracle's own and
     every directory above the root. Until 2026-10-20 a
     line judged only from the session's directory let `cd internal; rm
     ../reliability-profile.yaml` through. A `cd` through a variable into
     an oracle's directory followed by a bare name (`cd $D && rm s.txt`) is
     still allowed: counting it would deny every relative write after such
     a `cd`. Until 2026-10-19 every relative path was judged
     from the session's directory, so `cd <scratch repo> && git checkout -q
     -- .` was denied, and `cd snapshots && rm s.txt` was not. Since
     2026-10-20 a git write verb counts after git's global options too
     (`git -C <dir> checkout`, `git -c k=v restore`, `--git-dir`,
     `--no-pager`), with `-C`'s directory a place its paths resolve but
     never itself an operand; a path is judged both with `..` taken by name
     and as the kernel resolves it through symlinks, so `link/../x` names
     where the link leads; and where the file system ignores case (Windows,
     macOS) a protected glob matches in any case. Before, each of these let
     a write to a protected path through;
  3. on `git push` or `gh pr create` anywhere in the command, apply the
     [pre-push gate](/decisions/pre-push-v0.md).
- **stop**: allow when `stop_hook_active` is set. Block when product-class
  files changed since the newest inner or prepr report (diffing the report's
  tree against the current tree; when there is no report, against HEAD and
  against the tree the session started with, so a change the agent committed
  before stopping still counts), or when an open finding still needs the
  agent (`unclassified`, `new-code-defect`, `harness-defect`,
  `wrong-property`). Open findings that need a human do **not** block:
  stopping to ask is the right move.
- **session-start**: record the working tree's hash in
  `.reliab/sessions/<session id>` on a session's first start (resume and
  compact keep it), and add the profile, the rules, the latest report's
  freshness, and its open findings to context.
- **Acknowledgement.** `reliab findings ack <id> --by <who>` applies only to
  `legacy-defect-or-quirk`, `characterization-diff`, `equivalent-mutant`,
  `requirement-silent`, and `environment` findings and marks them
  acknowledged. An acknowledged finding cannot be reclassified.
- **Failure modes.** Outside a reliab repository hooks allow silently. With an
  invalid profile they allow, warn on stderr, and session-start says
  enforcement is off. A hook that cannot read its input exits 1, never 2,
  because Claude Code treats exit 2 as a blocking decision.

## Consequences

Acknowledgements live in the local reports only; see
[acks-are-local](/open-questions/acks-are-local.md).
