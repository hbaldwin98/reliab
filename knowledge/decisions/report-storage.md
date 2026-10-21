---
type: Decision
title: "Reports live in .reliab/reports/ and only reliab writes them"
description: "Reports are gitignored and denied to agent edits so an agent cannot forge a clean report."
status: stable
tags: [report, enforcement]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Reports live in .reliab/reports/ and only reliab writes them

## Decision

Reports go to `.reliab/reports/<tier>-latest.json` plus timestamped history.
The directory is gitignored. `reliab profile init` adds `Edit` and `Write`
deny rules on it, so only the reliab binary writes reports; otherwise an agent
could forge a clean report to satisfy the hooks.

**History is the durable record** (added 2026-10-18). Each run's copy is
`history/<tier>-<start time>.json`. Every later write to a latest report,
`findings classify`, `findings ack`, and `report verdict`, rewrites that
run's history copy too, re-scored the same way when a classification or
acknowledgement changes a mutation score, since the next run replaces the latest report and
a finding fixed in between leaves it. Found in step 3's real-use
acceptance: two classified findings were fixed, prepr re-ran, and their
classifications existed nowhere, so the PR summary listed them as fixed
but `unclassified`. A finding no latest report holds can still be
classified: `classify` then records it on the newest history report of
this branch that holds it, the run the PR summary reads, and clears the
latest pre-PR verdict
([finding-classification](/decisions/finding-classification.md)). History
files written earlier read as before; their lost classifications are not
recovered.

Approvals of protected-path changes live beside the reports in
`.reliab/approvals.json` ([protected-path-approval](/decisions/protected-path-approval.md)).
