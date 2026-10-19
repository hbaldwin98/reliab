---
type: Rule
title: "R7: Surviving mutants drive new tests"
description: "Surviving mutants on changed lines, not coverage gaps, drive new tests; equivalent-mutant calls are logged."
tags: [hard-rule]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§6.1; §5.10, Table 4"
---

# R7: Surviving mutants drive new tests

Surviving mutants on changed lines, not coverage gaps, drive new tests.
Equivalent-mutant judgments are logged with a one-line reason. If the
requirement is silent, ask.

**Why:** 49% of mutant-targeted tests in one industrial study added no line
coverage yet killed mutants the suite missed; suites with 100% coverage and 4%
mutation score have been reported.
