---
type: Rule
title: "R1: A failing test is a finding"
description: "The agent never changes an assertion, widens a tolerance, or adds a catch block to make a test pass."
tags: [hard-rule]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§6.1; §5.10, Table 4"
---

# R1: A failing test is a finding

A failing test written by the agent is a finding. The agent may change product
code to fix a confirmed new-code defect. It never changes an assertion, widens a
tolerance, or adds a catch block to make a test pass.

**Why:** repair and discard loops keep bug-validating tests (59.6–68.1% of
final suites in one 2024 study).

**Enforcement:** heuristic only. A post-edit test-weakened flag is deferred;
the mutation gate and the reviewer are the real backstops. See
[test-weakened-mutation-check](/open-questions/test-weakened-mutation-check.md).
