---
type: Decision
title: "Hooks remind and never block"
description: "reliab is a testing tool first, for a person at the CLI or an agent on its own. Hooks never deny: an edit to a protected path, a person-only reliab command, or a protected command or edit gets a reminder; so does a push; a stop gets one nudge. Shell writes are not inspected, and profile init writes no permission deny rules."
status: stable
tags: [hooks, enforcement, performance]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-21T10:00:00-07:00
---

# Hooks remind and never block

## Context

The paper's aim, and reliab's, is an agentic tool that tests changes and
improves a codebase's testing through skills and one CLI. By v0.1.5 the
guard had grown into a shell reader of about 3,700 lines
([command-effects](/decisions/command-effects.md)) that tracked `cd`,
wrappers, heredocs, and quoting in Bash and PowerShell to deny writes to
protected paths, plus a push gate that denied any push without a clean,
fresh, reviewed pre-PR report. Each round of review found new bypasses and
new false positives, and its mutants dominated reliab's own pre-PR
findings. The owner (2026-10-21): "I didn't want all the guardrails, I
wanted the testing", and "I don't want this tool to be blocking me from
doing anything valuable". reliab is a consolidated testing framework a
person runs from the CLI or an agent drives on its own.

## Decision

- **No hook denies.** Each answers with `additionalContext` the agent reads:
  - pre-edit: Edit, Write, and NotebookEdit on a protected path or
    `.reliab/`, and edits that add text matching `protected_edits`.
  - pre-bash: `reliab findings ack`, `approve`, and `baseline write`, and
    `report verdict` from anyone but the reviewer (`reliab:reviewer`, or a
    project copy named `reviewer`), when reliab is at a command position:
    the line's start or after `;`, `&`, `|`, `(`, a backtick, or `$(`,
    after any `VAR=value` assignments. Heredoc bodies are dropped first,
    so a commit message naming the command stays quiet.
  - pre-bash: commands matching the profile's `protected_commands`.
  - A `git ... push` or `gh pr create` gets `additionalContext` naming what
    the latest pre-PR report lacks (missing, stale, incomplete, open
    findings, a required verdict). CI is the gate.
  - Stop blocks once with a reminder when product code changed since the
    last report or the agent owes work on findings; `stop_hook_active`
    lets the next stop through. The harness delivers a Stop hook's message
    to the agent only as a block.
  - Session start briefs the workflow and the open findings.
- **Not inspected:** shell writes to protected paths. `reliab gate
  protected` in CI, and the human review of the diff, catch an oracle a
  shell command changed.
- `reliab profile init` writes no `Edit(...)` permission deny rules and no
  sandbox `denyWrite`, and doctor no longer checks for them; a user's own
  rules are kept.
- A reminder tells an agent what a person approves; when the user asked for
  the change, the agent goes ahead.

## Consequences

- `internal/guard/effects` and its tests are gone; PreBash is two regular
  expressions and the profile's own.
- Nothing stops an agent that ignores a reminder; `reliab gate protected`
  in CI and the person's review of the diff are where an unapproved oracle
  change is caught.
- Deprecated: [command-effects](/decisions/command-effects.md) and
  [pre-push-v0](/decisions/pre-push-v0.md). The pre-bash and stop parts of
  [hook-decisions-v0](/decisions/hook-decisions-v0.md) are replaced here.
- New hook work is a reminder. Effort goes into testing: running tests,
  and setting up each tool well per project.
