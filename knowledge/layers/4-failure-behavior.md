---
type: Layer
title: "Layer 4: Failure behavior"
description: "Does it fail safely, and is logic in the data store right? Oracle: no partial writes, bounded retries, no hangs; store-native assertions."
tags: [evidence-stack]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§3.2, Table 2; §5.9"
---

# Layer 4: Failure behavior

**Fault injection.** Does it fail safely? Oracle: no partial writes, bounded
retries, no hangs. Roles: [T8](/roles/t8-network-faults.md),
[T9](/roles/t9-http-stubs.md), [T10](/roles/t10-store-faults.md).

**Store-level tests.** Is logic that lives in the data store right? Oracle:
store-native assertions. Role: [T11](/roles/t11-store-tests.md).

The only layer that exercises paths production will eventually take and
ordinary tests never do. Inject combinations of faults, not only single ones
(89% of resilience bugs in one industrial study needed several), and seed every
injection so a failure replays exactly.
