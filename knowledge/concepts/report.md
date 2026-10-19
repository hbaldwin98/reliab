---
type: Concept
title: Report
description: The single team-owned JSON document every tier writes and the agent, hooks, reviewer, and CI read.
tags: [report, cli]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: §6.2, Listing 1; §9.1 P7
---

# Report

Every tool writes its native output; a format-keyed adapter converts it into
one report schema reliab owns. The agent reads only that schema. Owning it
keeps tools replaceable (principle P7): swapping a tool changes one adapter.

A report records the tier, budget, elapsed time, layers run, layers skipped
with reasons, completeness, the tree hash it ran against, and findings (id,
[kind](/concepts/finding-kind.md), layer, test, cited intent, replay input,
minimal input, observed, expected, status).

Related decisions: [schemas-and-versioning](/decisions/schemas-and-versioning.md),
[report-freshness](/decisions/report-freshness.md),
[report-storage](/decisions/report-storage.md).
