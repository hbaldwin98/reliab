---
type: Decision
title: "Enforcement hooks decide for themselves; CI gates are authoritative"
description: "Enforcement hooks match every relevant tool call without if filters and parse the command in reliab; in-session enforcement is best-effort and CI decides."
status: stable
tags: [hooks, enforcement, departure]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T15:35:50-07:00
---

# Enforcement hooks decide for themselves; CI gates are authoritative

## Context

Two hook behaviors are undocumented: whether a PreToolUse deny holds when the
user bypasses permissions, and exactly when an `if` filter falls back to
running the hook. The dangerous case for reliab is the opposite of the
fallback: an `if` filter such as `Bash(git push *)` that does not match
`bash -c "git push"`, so the hook never runs. The docs call `if` filtering
best-effort and point hard allow/deny at permission rules.

## Decision

- Enforcement hooks (`pre-edit`, `pre-bash`, `pre-push`) do **not** use `if`
  filters. They match every call of the relevant tool (`Edit|Write`, `Bash`),
  and `reliab hook <event>` parses the input and decides. This departs from the
  paper's Listing 6. Cost: one Go process start per matching call.
- `if` filters may still be used for hooks that only add context or log.
- In-session enforcement is best-effort. A user who bypasses permissions has
  chosen to weaken it. The agent-free gates (`reliab verify --tier prepr`,
  `reliab gate protected`) are authoritative, and the docs say so.

## Consequences

Closes [hook-deny-under-bypass](/open-questions/hook-deny-under-bypass.md)
without a spike. Replaces [hook-behavior-spike](/decisions/hook-behavior-spike.md).
