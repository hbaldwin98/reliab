---
type: Role
title: "T8: Network fault injection"
description: "Latency, drops, resets between the application and its stores."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T8: Network fault injection

**Must:** inject latency, drops, and resets between the application and its stores.

**Example fillings (illustrative, unverified):** Toxiproxy.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
