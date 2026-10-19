---
type: Rule
title: "R2: Protected oracles change only with human approval"
description: "Snapshots, the regression corpus, scrubbers, mutant suppressions, and hand-maintained API specs change only with human approval."
tags: [hard-rule]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§6.1; §5.10, Table 4"
---

# R2: Protected oracles change only with human approval

Characterization snapshots, the regression corpus, scrubbers, mutant
suppression lists, and any hand-maintained API specification change only with
human approval. The agent may draft a specification change; its owner approves.

**Why:** the cheapest way to fix a failing characterization test is to approve
the new output.

**Enforcement:** four layers; see
[protected-path-enforcement](/decisions/protected-path-enforcement.md).
