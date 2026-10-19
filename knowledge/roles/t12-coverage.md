---
type: Role
title: "T12: Coverage"
description: "Changed-lines coverage as a backstop; machine-readable output."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T12: Coverage

**Must:** measure changed-lines coverage as a backstop, with machine-readable output.

**Example fillings (illustrative, unverified):** cargo-llvm-cov, go test -cover, coverage.py, c8, JaCoCo.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
