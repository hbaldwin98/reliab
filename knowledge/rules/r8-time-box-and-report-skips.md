---
type: Rule
title: "R8: Time-box every run and report skips"
description: "Every run is time-boxed and reports what it skipped and why."
tags: [hard-rule]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§6.1; §5.10, Table 4"
---

# R8: Time-box every run and report skips

Every run is time-boxed and reports what it skipped and why. See
[tiers-budgets-exit-codes](/decisions/tiers-budgets-exit-codes.md); the stop
hook blocks when product code changed since the last report. A layer the
change cannot affect is skipped with that reason
([relevance-skips](/decisions/relevance-skips.md)).
