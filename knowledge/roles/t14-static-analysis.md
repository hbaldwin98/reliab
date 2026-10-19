---
type: Role
title: "T14: Static analysis"
description: "Warnings as errors on new and changed files; bug and style rules."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T14: Static analysis

**Must:** treat warnings as errors on new and changed files, with bug and style rules.

**Example fillings (illustrative, unverified):** clippy, golangci-lint, ruff and mypy, ESLint and tsc, Error Prone.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
