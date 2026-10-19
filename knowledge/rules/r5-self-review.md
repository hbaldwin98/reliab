---
type: Rule
title: "R5: Self-review before reporting"
description: "Check each new test reaches the changed code, cannot pass vacuously, and is grounded."
tags: [hard-rule]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§6.1; §5.10, Table 4"
---

# R5: Self-review before reporting

Before reporting, the agent reviews its own tests: does the test reach the
changed code; could it pass vacuously (no assertion, a swallowed exception, a
generator that never produces the interesting case); is the property grounded?

**Why:** self-review in one agentic property-testing study caught a test that
passed only because it was wrapped in a try/catch.
