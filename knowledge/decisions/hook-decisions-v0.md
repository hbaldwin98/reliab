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
     These match only text Bash may run. A Bash line whose every simple
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
     redirects (`>`, `>>`, numbered as in `2>`, `&>`, and `>|`; until
     2026-10-20 a numbered or `&` redirect and `>|` were missed) to such a
     path, or a write verb (`sed -i`, `cp`,
     `mv`, `rm`, `tee`, `git checkout|restore`, `Set-Content`,
     `Remove-Item`, and similar) followed by one in the same simple command
     (the command line split at `;`, `&`, `|`, and newlines). Heuristic and
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
     session's directory. Until 2026-10-19 every relative path was judged
     from the session's directory, so `cd <scratch repo> && git checkout -q
     -- .` was denied, and `cd snapshots && rm s.txt` was not;
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
