---
type: Role
title: "T6: Typed API client"
description: "Call the application from tests with its own request types."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T6: Typed API client

**Must:** call the application from tests using its own request types.

**Example fillings (illustrative, unverified):** the application's own protocol client, a generated OpenAPI client, httpx with models.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
