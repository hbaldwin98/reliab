---
type: Layer
title: "Layer 3: Mutation testing"
description: "Would the tests catch a bug here? Oracle: mutants killed by the suite."
tags: [evidence-stack]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§3.2, Table 2; §5.1"
---

# Layer 3: Mutation testing

**Question:** Would the tests catch a bug here?
**Oracle:** mutants killed by the suite.
**Role:** [T4](/roles/t4-mutation.md).

Mutate changed lines only, suppress mutants in arid code (logging, timing,
configuration), and feed the agent surviving mutants rather than coverage gaps
([R7](/rules/r7-survivors-drive-tests.md)). Mutation score is the quality gate
for tests the agent writes. For a maintained codebase the first run is a
baseline, not a gate ([mutation-baseline-first](/decisions/mutation-baseline-first.md)).
