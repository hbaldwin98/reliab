---
type: Concept
title: Evidence stack
description: Layers of evidence, each answering one question with its own independent oracle.
tags: [method]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: §3.2, Table 2
---

# Evidence stack

No single technique proves code correct. Each layer answers one question with
its own oracle, so layers fail independently; agreement is strong evidence and
disagreement is itself a finding.

| Layer | Concept |
|---|---|
| 0 | [Static analysis](/layers/0-static-analysis.md) |
| 1 | [Characterization and regression corpus](/layers/1-characterization.md) |
| 2 | [Specification properties](/layers/2-specification.md) |
| 3 | [Mutation testing](/layers/3-mutation.md) |
| 4 | [Failure behavior](/layers/4-failure-behavior.md) |
| 5 | [Differential replay](/layers/5-differential-replay.md) |

The key distinction is between **characterization** tests, which pin actual
behavior on purpose (bugs included), and **specification** tests, which state
intended behavior. A conflict between the two is never resolved by the agent;
it goes to a human ([R9](/rules/r9-escalate-pinned-behavior.md)).

Which layer leads depends on the [starting condition](/concepts/starting-condition.md).
Which layers run where depends on the [tier](/concepts/tier.md).
