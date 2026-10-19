---
type: Concept
title: Tier
description: A time-boxed run of verify; inner loop, pre-PR, or nightly, each with its own budget and layer mix.
tags: [method, cli]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: §6, Table 5, Figure 1
---

# Tier

| Tier | Trigger | Budget | Output |
|---|---|---|---|
| inner | A completed change, before the agent stops ([inner-tier-trigger](/decisions/inner-tier-trigger.md)) | < 5 min | Pass, or a reproduced and classified failure |
| prepr | Before a pull request | 10–15 min | PR summary: layers run, seeds, survivors, findings, skips |
| nightly | Schedule or on demand | Hours | Triaged, de-duplicated, minimized findings; new corpus entries |

Cheap layers run fully everywhere; open-ended layers are sampled or limited to
changed code inside the time-boxed tiers and run fully only at night. The
paper's Figure 1 is reliab's built-in default layer mix per tier, overridable
in the profile; see [tiers-budgets-exit-codes](/decisions/tiers-budgets-exit-codes.md).

Agent-free CI runs the same binary at the prepr tier on every push and the
nightly tier on a schedule.
