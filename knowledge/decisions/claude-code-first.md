---
type: Decision
title: "Claude Code first, with its hook protocol isolated"
description: "Target Claude Code, but keep its hook stdin/stdout formats in one internal package."
status: stable
tags: [plugin, hooks]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Claude Code first, with its hook protocol isolated

## Decision

reliab targets Claude Code's plugin model. Claude Code's hook input and output
JSON live in one internal package; the rest of the CLI deals only in decisions
(allow, deny with reason, block stop with reason, add context). Another
harness later means another adapter package.
