---
type: Decision
title: "v0 pre-push gate without a reviewer verdict"
description: "Pre-push requires a fresh, complete pre-PR report with no blocking findings; the verdict check waits behind a flag."
status: deprecated
tags: [hooks, departure]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# v0 pre-push gate without a reviewer verdict

> Deprecated 2026-10-21: replaced by [light-guardrails](/decisions/light-guardrails.md).

## Context

The paper's pre-push hook also requires a reviewer verdict; the reviewer
subagent is deferred from v0.

## Decision

In v0, `reliab hook pre-push` denies `git push` and PR creation unless a prepr
report is [fresh](/decisions/report-freshness.md), complete, and has no
blocking findings. The verdict check exists behind the profile flag
`require_reviewer_verdict: false`, turned on when the reviewer ships.

The reviewer shipped on 2026-10-18: with the flag on, the gate also needs a
passing verdict for the report's own tree
([reviewer-verdict](/decisions/reviewer-verdict.md)).
