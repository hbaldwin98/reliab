---
type: Role
title: "T4: Mutation testing"
description: "Mutate changed lines only; report survivors per line; threshold gate."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T4: Mutation testing

**Must:** mutate changed lines only, report survivors per line, and support a threshold gate.

**Example fillings (illustrative, unverified):** cargo-mutants, gremlins, go-mutesting, Stryker, mutmut, PIT.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
