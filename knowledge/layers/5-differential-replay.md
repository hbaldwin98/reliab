---
type: Layer
title: "Layer 5: Differential replay"
description: "Does new code match old on real inputs? Oracle: outputs and state of the previous build or code path."
tags: [evidence-stack]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§3.2, Table 2; §5.8; §9"
---

# Layer 5: Differential replay

**Question:** Does new code match old on real inputs?
**Oracle:** outputs and state of the previous build or code path.
**Roles:** the replay harness (custom per profile),
[T13](/roles/t13-shadow-experiments.md).

Set a correctness target and size the corpus to reach it. Compare read paths
with a shadow-experiment library and write paths by replay in a test
environment. Also the oracle for a platform move (§9): replay the corpus
against both builds and accept zero unexplained differences.
