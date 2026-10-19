---
type: Decision
title: "Spike undocumented hook behavior before building hook logic"
description: "Superseded: the design no longer depends on the answers, so the spike was dropped."
status: deprecated
tags: [hooks, spike]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Spike undocumented hook behavior before building hook logic

**Deprecated 2026-10-17.** Replaced by
[enforcement-hooks-self-contained](/decisions/enforcement-hooks-self-contained.md).
Neither open behavior changes the design once enforcement hooks stop relying
on `if` filters and CI gates are treated as authoritative, so the spike was
not run.

## Context

The docs (checked 2026-10-17) leave open whether a PreToolUse hook deny holds
when permissions are bypassed, and exactly when `if` filters fall back to
running the hook anyway.

## Decision (as originally made)

In week one, build a throwaway plugin that exercises these cases and record the
results as Decisions here before writing the hook logic. Tracked in
[hook-deny-under-bypass](/open-questions/hook-deny-under-bypass.md).
