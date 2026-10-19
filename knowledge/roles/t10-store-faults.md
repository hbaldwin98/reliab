---
type: Role
title: "T10: Data-store fault injection"
description: "Seeded failures at the connection or pool, inside the application process."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T10: Data-store fault injection

**Must:** seed failures at the connection or pool, inside the application process.

**Example fillings (illustrative, unverified):** a wrapping connection type written per profile.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
