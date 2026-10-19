---
type: Role
title: "T7: API fuzzing from specification"
description: "Generate requests from the API description; flag server errors and schema drift."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T7: API fuzzing from specification

**Must:** generate requests from the API description and flag server errors and schema drift.

**Example fillings (illustrative, unverified):** Schemathesis; where no spec exists, a property or fuzz target over the request type.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
