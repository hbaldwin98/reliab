---
type: Concept
title: Stack profile
description: The one checked-in file (reliability-profile.yaml) that binds the method to one codebase.
tags: [profile]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: §4.1; Appendix A Listing 4
---

# Stack profile

`reliability-profile.yaml`, read by the CLI, the agent, and CI. It declares:

- the [starting condition](/concepts/starting-condition.md)
- current and target runtimes and the lowest runtime both support
- one entry per [role](/roles/) T1–T14: tool, version, the date it was verified
  on this codebase, or `applicable: false` with a reason
- `commands:` — the only place a tool is named; verify runs these strings
- path classification (`paths.product`, `paths.tests`, `paths.ignore`;
  see [path-classification](/decisions/path-classification.md))
- the impacted-tests map ([impacted-tests-map](/decisions/impacted-tests-map.md))
- protected paths, tier budgets and overrides
- an append-only `corrections` list: tool, what its metadata claimed, what was
  found, the decision, the source, the date

The paper calls the corrections list "the most valuable part of a profile and
the one no template can pre-fill." Validation rules are in
[schemas-and-versioning](/decisions/schemas-and-versioning.md).
