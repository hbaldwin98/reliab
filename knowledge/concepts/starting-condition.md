---
type: Concept
title: Starting condition
description: Greenfield, maintained, or legacy; declared in the profile, it decides which evidence layer leads.
tags: [method, profile]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: §3.1, Table 1; §11
---

# Starting condition

| Condition | What the oracle is | What leads |
|---|---|---|
| greenfield | Stated intent only: tickets, specs, guidelines | Properties and mutation from the first endpoint; characterization grows as endpoints ship |
| maintained | Intent plus an existing suite of unknown adequacy | Pin the most-used surface first; a mutation run over the existing suite is the first finding |
| legacy | Yesterday's behavior; intent partly lost | Characterization first and largest; legacy-versus-intended decisions go to a product owner |

The contract, hooks, and CI gates are identical in all three; only the order
and weight of the adoption stages change (§11). Adoption order:

- legacy: S0–S6 as numbered
- greenfield: S0, S1, S3, then S2 as endpoints ship
- maintained: S0, S1, then a mutation baseline over the existing suite before S2

reliab itself is **greenfield**; its first consumer repository is
**maintained** ([dogfood-targets](/decisions/dogfood-targets.md)).
