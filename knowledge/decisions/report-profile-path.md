---
type: Decision
title: "A report names the profile file it ran with"
description: "verify records the profile file it loaded as run.profilePath (report 0.12.0). Commands that judge a report (summary, gate mutation, classify's re-score, the verdict's freshness check) load that file; protection (gate protected, approve, the hooks, the summary's protected-path list) always reads reliability-profile.yaml."
status: draft
tags: [report, profile, verify]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-20T19:00:00-07:00
---

# A report names the profile file it ran with

## Context

`reliab verify -f alt.yaml` runs with another profile, but the report kept
only the profile's name. `report summary`, `gate mutation`, the re-score
after `findings classify`, and `report verdict`'s freshness check all
loaded `reliability-profile.yaml` instead. A run whose profile left a path
out of the tree hash read as stale, a tier's commands were listed against
the wrong profile, and a mutation score was re-judged by thresholds the run
never used: false failures from the tool's own bookkeeping.

## Decision

- **verify records the file**: `run.profilePath`, slash-separated and
  relative to the repo root (absolute when outside it). Report 0.12.0; the
  field is optional, and a report without it reads as
  `reliability-profile.yaml`, as before.
- **Commands that judge a report load that file**, through
  `Report.ProfileFile`: the summary's freshness, tier list, and base; `gate
  mutation`'s thresholds, tier, and freshness; the mutation re-score when a
  classification changes a count; the verdict's freshness check, for the
  author's report and the reviewer's re-run alike. `findings classify` reads
  the base from the latest pre-PR report's profile, as the summary of it
  does.
- **Protection stays on the default profile.** `gate protected`, `reliab
  approve`, the hooks, and the summary's list of protected-path changes read
  `reliability-profile.yaml`, which protects itself
  ([protected-path-approval](/decisions/protected-path-approval.md)). A
  second, unprotected profile must not narrow what is protected.

## Consequences

- `gate mutation` judges the profile the report ran with. On CI that is the
  file CI passed to verify. Locally, a run with an unprotected second
  profile can be judged by its thresholds; the gate's authority is CI's run,
  not a local report.
- `baseline propose` and `baseline write` still read the default profile,
  since the baseline file and its path are protected.
