---
type: Rule
title: "R6: Rank findings before a human sees them"
description: "Findings are scored on a rubric; only the top band interrupts a human."
tags: [hard-rule]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§6.1; §5.10, Table 4"
---

# R6: Rank findings before a human sees them

Findings are scored against a rubric (reproducible, grounded, user-visible,
severity). Only the top band interrupts a human; the rest wait in the queue.

**Why:** ranking lifted valid reports from 56% to 86% in one study; 8 of 41
candidate catches were real in another.

The rubric is not yet defined: [r6-scoring-rubric](/open-questions/r6-scoring-rubric.md).
