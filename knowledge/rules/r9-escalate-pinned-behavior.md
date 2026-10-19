---
type: Rule
title: "R9: Escalate behavior a snapshot pins"
description: "Behavior that looks wrong but is pinned by a characterization test is escalated, never fixed by the agent."
tags: [hard-rule]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§6.1; §5.10, Table 4"
---

# R9: Escalate behavior a snapshot pins

Behavior that looks wrong but is pinned by a characterization test is
escalated, never fixed by the agent. Only someone who knows the clients can say
which behavior is correct.

**Why:** in one agentic property-testing study, a reported bug was judged to be
intended behavior.
