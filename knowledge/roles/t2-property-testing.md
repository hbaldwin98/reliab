---
type: Role
title: "T2: Property-based testing"
description: "Generators per input type, shrinking, seed replay, callable from T1."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T2: Property-based testing

**Must:** provide generators per input type, shrinking, and seed replay, callable from T1.

**Example fillings (illustrative, unverified):** proptest, rapid, Hypothesis, fast-check, jqwik, FsCheck, CsCheck.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
