---
type: OpenQuestion
title: "What exactly is the canary break"
description: "The paper names reliab canary but never defines which bug is injected, where, or how it is reverted."
status: draft
tags: [canary, deferred]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# What exactly is the canary break

The paper's reviewer runs `reliab canary`, a deliberate break that must make the
suite fail. Undefined: which mutation, where (changed lines? a property's
target?), how it is applied without touching the working tree (a worktree?),
and how it is reverted. Deferred with the reviewer; see
[v0-scope](/decisions/v0-scope.md).
