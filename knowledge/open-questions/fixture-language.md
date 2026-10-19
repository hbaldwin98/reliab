---
type: OpenQuestion
title: "Which language is the testdata fixture service"
description: "The fixture under testdata/ serves reliab own CLI tests; its ecosystem is not chosen yet."
status: draft
tags: [targets, fixture]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Which language is the testdata fixture service

The fixture should exercise an ecosystem other than Go (reliab) and the
first consumer repository's, to keep reliab honest about stack neutrality,
and must be fast enough for reliab's own test suite. See [dogfood-targets](/decisions/dogfood-targets.md).
