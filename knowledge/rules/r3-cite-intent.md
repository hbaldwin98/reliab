---
type: Rule
title: "R3: Every property cites its source of intent"
description: "Each specification property names a ticket, document, rule card, or dependent caller in a comment."
tags: [hard-rule]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§6.1; §5.10, Table 4"
---

# R3: Every property cites its source of intent

Every specification property names its source of intent in a comment: a
ticket, a document, a reviewed rule card, or a caller that depends on the
behavior.

**Why:** LLM-written oracles follow actual rather than intended behavior, and
buggy code in the prompt cut bug-revealing tests from 8.51% to 2.98% in one
2026 study. Show intent before implementation.
