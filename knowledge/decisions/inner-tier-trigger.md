---
type: Decision
title: "The inner tier runs when a change is done, not after each edit"
description: "Agents run verify --tier inner once a logical change is complete, before stopping; the stop hook enforces it. Mid-change edits are never interrupted."
status: stable
tags: [tiers, departure]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T19:20:00-07:00
---

# The inner tier runs when a change is done, not after each edit

## Context

The paper's inner tier is triggered by "every agent edit" (Table 5), and the
first wording of the AGENTS.md rules, the verify skill, and the session brief
said "after every edit". Read literally, that is a test run after every Edit
call, which stalls an agent halfway through a multi-file change.

## Decision

The agent runs `reliab verify --tier inner` once a logical change is done,
before it stops or hands back, and not after each file edit. The stop hook
already enforces exactly this: it blocks stopping while product code changed
since the last report ([hook-decisions-v0](/decisions/hook-decisions-v0.md)).
`--tier prepr` before a PR is unchanged.

## Consequences

The AGENTS.md template, the verify skill, the session-start brief, and the
README say "when a change is done". Repositories already set up refresh their
AGENTS.md section by re-running `reliab profile init`.
