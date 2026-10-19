---
type: OpenQuestion
title: "Replace the test-weakened heuristic with a mutation check"
description: "Instead of pattern-matching removed assertions, re-run the mutants the removed assertion used to kill."
status: draft
tags: [rules, enforcement, deferred]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Replace the test-weakened heuristic with a mutation check

The paper's post-edit hook flags removed assertions, widened tolerances, or new
catch blocks as test-weakened ([R1](/rules/r1-failing-test-is-a-finding.md)),
heuristically. Its open question 4 asks whether to re-run the mutants the
removed assertion used to kill instead. Needs mutation results per test,
which v0 does not track.
