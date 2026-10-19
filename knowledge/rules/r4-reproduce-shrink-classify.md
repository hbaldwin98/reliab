---
type: Rule
title: "R4: Reproduce, shrink, classify"
description: "Every failure is reproduced from its seed or saved input, shrunk, and classified before it is reported."
tags: [hard-rule]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§6.1; §5.10, Table 4"
---

# R4: Reproduce, shrink, classify

Every failure is reproduced from its seed or saved input, shrunk, and
classified before it is reported. Crashes are classified as harness or product
defects. See [finding kind](/concepts/finding-kind.md) and
[finding-classification](/decisions/finding-classification.md).
