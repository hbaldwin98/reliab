---
type: Role
title: "T5: In-process test host"
description: "Start the application inside the test process; one adapter class per runtime."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T5: In-process test host

**Must:** start the application inside the test process, through one adapter per runtime.

**Example fillings (illustrative, unverified):** an in-process daemon on a temp socket, httptest, TestServer, supertest, MockMvc.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
