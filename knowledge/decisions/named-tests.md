---
type: Decision
title: "Named tests: verify takes the tests that guard the change per run"
description: "verify --tests (one test run per flag) and --mutation-tests (comma-separated) name the tests that guard the change, in any tier, with or without --only. reliab passes them through as opaque text in {tests}, {mutation_tests}, RELIAB_TESTS, and RELIAB_MUTATION_TESTS, records them in the report, and a command whose profile needs them, run without them on a relevant change, leaves the run incomplete with reason needs-input."
status: draft
tags: [cli, verify, report, profile, hooks]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-20T12:00:00-07:00
---

# Named tests: verify takes the tests that guard the change per run

## Context

A tier runs a fixed set of profile commands over the change. The only
test selection that reached a command was `{impacted}`, a static map from
changed paths to test arguments
([impacted-tests-map](/decisions/impacted-tests-map.md)). On a large
codebase that is far too coarse: one changed data-layer file maps to a
dozen test projects, and a mutation tool asked to run against all of them
discovered thousands of tests and ran out of memory before mutating
anything. Naming the few test classes that exercise the changed file
brought the same run to minutes. The agent knows which tests guard its
change; reliab had no way to take that per run.

`--scope` with `--only` ([on-demand-verify](/decisions/on-demand-verify.md))
narrows by paths, not tests, and writes an on-demand report that never
satisfies the hooks, so it cannot be a PR's evidence. The workaround,
environment variables a profile helper read, was shell-specific, leaked
from one run to the next, was invisible to the report, and enforced "name
your tests" by failing the command, which read as a defect.

## Decision

- **`--tests <spec>`** is one test run, in the syntax of the profile's
  runner; repeat it for more runs. It is never comma-split, since a spec
  may hold a filter with commas. **`--mutation-tests <names>`** names the
  tests mutation runs against, comma-separated or repeated. Both work in
  every tier, with or without `--only`. reliab trims each value, rejects an
  empty one, and otherwise treats them as opaque: the profile's comments
  document their syntax, so the core stays stack-agnostic.
- **Placeholders and environment, quoted once by reliab.** `{tests}` and
  `{mutation_tests}` expand to one POSIX sh word per item, single-quoted,
  space-separated: commands run under `sh`
  ([verify-execution](/decisions/verify-execution.md)), so a runner script
  receives each spec as one argument whatever it contains. Placeholders are
  substituted in one pass, so a spec that contains `{seed}` reaches the
  command as written. `RELIAB_TESTS` and `RELIAB_MUTATION_TESTS` carry the
  same lists as JSON arrays of strings, `[]` when none were named. verify
  always sets them, so a value left in the caller's shell never applies.
- **A command can need them.** A profile command may declare
  `needs: [tests]`, `[mutation_tests]`, or both. When one is not named and
  the command would run (no `relevant_when`, or the change matches it), it
  is skipped with reason `needs-input` and a detail naming the flag, the
  run is `complete=false`, and the commands after it still run. A missing
  input is not a failure and not a finding. On a change the command cannot
  affect it is skipped as not relevant, as before
  ([relevance-skips](/decisions/relevance-skips.md)). It applies in every
  tier, nightly and on-demand included: the profile chose to need the
  input.
- **The report records the selection** as `run.tests` and
  `run.mutationTests`, as given. `reliab report summary` lists them under
  the run line and says an incomplete run waits for tests, not the budget;
  `reliab report route` prints them and each command that waits. verify
  exits 3 and names the waiting commands.
- **A tier run with named tests is a normal tier run.** It satisfies the
  stop hook and the push gate like any other, unlike `--only`. While the
  latest inner or pre-PR report has a `needs-input` skip, the stop hook
  blocks with the commands that wait, and the push gate denies, naming
  them.
- **A different selection does not back a report.** There is no current
  selection outside a run for a hook to compare against: the selection is
  part of the report, and a re-run with other tests writes a new report
  (the old one's verdict does not carry). Where two runs must agree, they
  are compared: `reliab report verdict` refuses a pass when the reviewer's
  re-run named other tests or mutation tests than the author's report, and
  the reviewer agent passes the author's selection to its re-run unchanged
  ([reviewer-verdict](/decisions/reviewer-verdict.md)).
- **The reviewer judges the selection.** Its checklist adds that every
  changed product file has a named test that reaches it, read from the
  report against the diff.
- Report 0.11.0 adds `run.tests`, `run.mutationTests`, and the skip reason
  `needs-input`; the profile adds a command's `needs`. All are optional.

## Consequences

- **Tests named too narrowly cannot fake a pass.** A selection only removes
  tests. Fewer tests can kill fewer mutants and cover fewer lines, never
  more, so survivors and uncovered lines still surface and the mutation
  threshold still holds. What a narrow selection can do is skip evidence:
  a changed file no named test reaches goes unexercised. That is why the
  selection is in the report and the summary, and why the reviewer checks
  it against the diff. A `coverage` command should keep running whole
  projects rather than read `{tests}`.
- A profile command that ignores `{tests}` and `RELIAB_TESTS` runs as
  before; naming tests changes nothing for it.
- A profile that needs tests for a command makes every run without them
  incomplete, CI's included: a tier CI runs without naming tests should
  not list such commands.
- Not done: a profile flag that requires a named selection for `prepr`
  whenever product paths changed. `needs` on the commands that need one
  covers the cases seen so far.
