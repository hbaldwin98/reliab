---
type: Decision
title: "A report names the profile file it ran with"
description: "verify records the profile file it loaded as run.profilePath (report 0.12.0). Commands that judge a report (summary, gate mutation, classify's re-score, the verdict's freshness check) load that file when reliability-profile.yaml protects it, and the default otherwise; protection (gate protected, approve, the hooks, the summary's protected-path list) always reads reliability-profile.yaml."
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
- **Commands that judge a report load that file when the default profile
  protects it**, through `Report.JudgeProfile`: the summary's freshness, tier list, and base; `gate
  mutation`'s thresholds, tier, and freshness; the mutation re-score when a
  classification changes a count; the verdict's freshness check, for the
  author's report and the reviewer's re-run alike. `findings classify` reads
  the base from the latest pre-PR report's profile, as the summary of it
  does.
- **An unprotected file does not judge its own report.** When
  `reliability-profile.yaml` does not protect the recorded file (or it is
  outside the repo), those commands judge by the default profile, as before
  0.12.0. An agent can write a second profile; if it judged its own report,
  that file could exclude product code from the tree hash, lower a
  threshold, and leave out the baseline, and the summary, `gate mutation`,
  and the verdict would all pass a stale, under-scored change. A protected
  file needed a person's approval to change, so it is trusted as the default
  is. The summary names the recorded file and the one it judged by.
- **Protection stays on the default profile.** `gate protected`, `reliab
  approve`, the hooks, and the summary's list of protected-path changes read
  `reliability-profile.yaml`, which protects itself
  ([protected-path-approval](/decisions/protected-path-approval.md)). A
  second, unprotected profile must not narrow what is protected.

## Consequences

- `gate mutation` judges the profile the report ran with when it is
  protected; a CI profile beside the default belongs in `protected_paths`.
  A run with an unprotected second profile is judged by the default, so a
  narrower file reads stale or short rather than passing.
- The hooks still judge freshness with the default profile's excludes, so
  for a protected second profile with other excludes the hooks and the
  summary can disagree about the same report.
- `baseline propose` and `baseline write` still read the default profile,
  since the baseline file and its path are protected.
