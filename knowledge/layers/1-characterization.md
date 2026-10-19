---
type: Layer
title: "Layer 1: Characterization and regression corpus"
description: "Did existing behavior change, and do past failures stay fixed? Oracle: recorded behavior and saved inputs."
tags: [evidence-stack]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§3.2, Table 2; §2.1"
---

# Layer 1: Characterization and regression corpus

Two techniques share this layer.

**Characterization (golden master).** Did existing behavior change? Oracle:
recorded responses and persisted state; scrubbed real inputs replayed. Roles:
[T3](/roles/t3-snapshot.md), [T5](/roles/t5-in-process-host.md),
[T6](/roles/t6-typed-client.md). Pins actual behavior on purpose, bugs
included; protects behavior but cannot say whether it is right.

**Regression corpus.** Do past failures stay fixed? Oracle: saved seeds and
failing inputs. Role: [T1](/roles/t1-unit-test-framework.md). Replayed fully in
every tier.

Snapshots, the corpus, and scrubbers are protected
([R2](/rules/r2-protected-oracles.md)); needs no understanding of the code.
