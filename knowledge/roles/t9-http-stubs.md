---
type: Role
title: "T9: External HTTP stubs"
description: "Scripted 500s, timeouts, malformed bodies per dependency."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T9: External HTTP stubs

**Must:** script 500s, timeouts, and malformed bodies per external dependency.

**Example fillings (illustrative, unverified):** WireMock, MSW, responses, nock.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
