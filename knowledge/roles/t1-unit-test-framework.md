---
type: Role
title: "T1: Unit test framework"
description: "Hosts every other role's tests; normally the framework already in use."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T1: Unit test framework

**Must:** host every other role's tests; normally the framework already in use.

**Example fillings (illustrative, unverified):** cargo test / nextest, go test / gotestsum, pytest, Jest, Vitest, JUnit, xUnit, NUnit.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
