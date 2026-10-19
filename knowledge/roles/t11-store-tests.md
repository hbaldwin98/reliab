---
type: Role
title: "T11: Data-store-level tests"
description: "Test procedures, triggers, constraints where logic lives in the store."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T11: Data-store-level tests

**Must:** test procedures, triggers, and constraints where logic lives in the store.

**Example fillings (illustrative, unverified):** pgTAP, tSQLt, utPLSQL.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
