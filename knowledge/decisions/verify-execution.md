---
type: Decision
title: "How verify runs commands"
description: "Commands run under sh from the repo root with logs in .reliab/out; outputs may be globs; a failed build skips the rest; budget overruns kill the whole process tree; finding ids are stable so classifications carry forward."
status: stable
tags: [cli, verify]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T17:10:00-07:00
---

# How verify runs commands

## Decision

- **Shell.** Every profile command runs as `sh -c "<command>"` from the repo
  root, on every OS, so one profile works everywhere. On Windows that is Git
  Bash (`sh` on PATH, else Git for Windows' default location); without it
  verify stops with a config error.
- **Placeholders and environment.** `{impacted}`, `{cases}`, `{seed}`,
  `{scope}`, and `{base}` are substituted in the command string, and
  `RELIAB_TIER`, `RELIAB_SEED`, `RELIAB_PROPERTY_CASES`, `RELIAB_SCOPE`, and
  `RELIAB_BASE` are exported, plus `RELIAB_COMMAND_START` (RFC 3339, the
  command's own start, so an adapter can ignore files an earlier run left),
  and `RELIAB_CAPTURE_DIR` (with `{capture}`) for the `characterize` command
  ([on-demand-characterization](/decisions/on-demand-characterization.md)).
  `{impacted}` narrows only in the inner tier, or to an on-demand run's
  scope ([impacted-tests-map](/decisions/impacted-tests-map.md),
  [schema-v0-shape](/decisions/schema-v0-shape.md),
  [on-demand-verify](/decisions/on-demand-verify.md)); ignored and
  freshness-excluded files do not widen it.
- **Logs and output.** Each command's stdout and stderr go to
  `.reliab/out/<tier>-<id>.log`. A command's `output` is a path or a glob;
  every match is deleted before it runs, so a stale result is never parsed,
  and every match is read after
  ([external-adapters](/decisions/external-adapters.md)).
- **Base.** Resolved once, only when a command uses it (`changed_only` or
  `{base}`): `--base`, else the profile's `base`, else the first fallback
  ref with a merge base. An unresolvable base is a config error; the report
  records the base it used as `run.base`.
- **A failed build stops the run.** When the `build` command yields a
  finding, every later command is skipped with reason `build-failed`: it
  would test the previous build's binaries and report their results as
  this tree's. The build failure is itself a finding, so the run exits 1
  and never reads as a pass; route and the summary list the skipped
  commands. Only the `build` id does this; other commands keep running
  after a failure.
- **Relevance.** In the inner and prepr tiers a command whose
  `relevant_when` globs match no file changed since the base is skipped
  with reason `not-relevant`, never in nightly, under `--only`, or without
  a base ([relevance-skips](/decisions/relevance-skips.md)).
- **Budget.** The deadline is the tier budget from the start of the run. When
  it passes, the running command's whole process tree is killed (a process
  group on Unix, a job object on Windows, because `taskkill /T` took about 14
  seconds to walk a tree in testing). The killed command and every later one
  are skipped with reason `budget`, and the report is incomplete.
- **Findings.** A command with a parsed format yields one finding per failing
  test; otherwise, or when the tool failed without reporting a failing test,
  a nonzero exit yields one finding carrying the log tail. Ids are stable,
  `F-<command>` or `F-<command>-<hash of test name>`, so the next run of the
  same tier carries forward the agent's classification and any human
  acknowledgement for findings that are still present.
- **Tree hash.** Taken at the start of the run, always excluding `.reliab/**`
  so reliab's own output never makes a report stale
  ([report-freshness](/decisions/report-freshness.md)).
- **Exit.** `1` if any finding is open, else `3` if the run is incomplete,
  else `0` ([tiers-budgets-exit-codes](/decisions/tiers-budgets-exit-codes.md)).
