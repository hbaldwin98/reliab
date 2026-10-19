---
type: Decision
title: "Tier defaults, budget overruns, and exit codes"
description: "Figure 1 is the built-in tier default, overridable in the profile; overruns mark the report incomplete; exit codes 0 to 3."
status: stable
tags: [cli, tiers]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Tier defaults, budget overruns, and exit codes

## Decision

- The paper's Figure 1 is built into the CLI as each
  [tier](/concepts/tier.md)'s default layer mix; the profile overrides it with
  `tiers.<tier>.layers`.
- A layer whose command is missing from the profile is recorded as skipped with
  reason "no command"; it is not an error.
- When the budget runs out, the running command is killed, every remaining
  layer is recorded as skipped with reason "budget", and the report is marked
  `complete: false` ([R8](/rules/r8-time-box-and-report-skips.md)).
- Exit codes: `0` passed and complete; `1` open findings; `2` config or usage
  error; `3` incomplete (budget) with no findings.
