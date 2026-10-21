---
type: Decision
title: "Onboarding: stage S0 as a skill the agent runs"
description: "The plugin's onboard skill (/reliab:onboard) walks an agent through S0 on any repository: init with the starting condition, a read-only inventory, repeated suite runs that list flaky tests, a probe through reliab verify for every role filled, tiers wired within budget, and a stop at the S0 gate with a report. It never edits product code or existing tests. Example profiles for Go, Rust, and .NET ship in templates/examples and beside the skill."
status: draft
tags: [plugin, profile, adoption]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T20:00:00-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4.2 The verification protocol; §11, Table 14 (S0)"
---

# Onboarding: stage S0 as a skill the agent runs

## Context

`reliab profile init` writes a skeleton whose roles all read "not filled
yet". Filling it (stage S0: probe each role on the real codebase, record
what fails, measure timings) was left to the person, who had to know the
method's verification protocol to do it. For v0.1.0 a
person should be able to hand S0 to an agent and have it done in under a
day, on any stack and any [starting condition](/concepts/starting-condition.md).

## Decision

- **A user-invocable `onboard` skill** (`plugin/skills/onboard`,
  `/reliab:onboard [condition]`), in the style of `mutation`, `properties`,
  and `verify`. No new CLI command: the steps need judgment (which tool,
  which filter, whether a probe really passed), and every step already has
  a CLI primitive (`profile init`, `verify --only`, `doctor`, `report
  route`, `findings classify`).
- **Ask only what the code cannot tell**: the starting condition, which
  candidate oracles are protected, who approves them, and a planned runtime
  move. The agent looks first and recommends an answer for each.
- **A role is filled only after a probe** ran through `reliab verify --only`
  on this repository and reliab ingested its output, including a deliberate
  failure that became a finding naming the right test or line. Otherwise the
  role is `applicable: false` with a reason, or the failed filling is a
  `corrections` entry and the next candidate is probed. Package metadata
  only chooses what to probe. Versions come from the tool itself.
- **Probes that need code run in a scratch git worktree** outside the
  repository and are discarded, so S0 never edits product code or existing
  tests. Installing a tool into the repository, adding a library, or
  converting a project is a question for the person.
- **Flakiness is measured, never hidden**: the suite runs three times (five
  if a run is under a minute) with no budget; a test failing in some runs is
  classified `legacy-defect-or-quirk` with "failed N of M", one failing in
  every run as an existing failure, and both are listed for the person.
  Nothing is quarantined, skipped, or retried.
- **Tiers are wired from measured times**: inner under 5 minutes with the
  impacted map, each map entry checked with `--scope`, slow commands moved
  to prepr, and `protected_paths`, `protected_commands`, and
  `protected_edits` set for the tools in use.
- **The skill stops at the S0 gate** after `doctor`, inner, and prepr, with
  a report: profile, roles, timings, the suite and its flaky tests, open
  questions, skips, and next steps. It does not start S1, add tests, or
  commit.
- **The report's next steps** name the CI commands (`reliab docs ci`) and
  what leads by starting condition: for maintained, a mutation baseline
  area by area, which the agent proposes with `reliab baseline propose` and
  a person writes; for legacy, characterization of the most-used surface;
  for greenfield, properties and mutation on the first endpoint. The skill
  ended at the gate with no next step, and a maintained repository's first
  finding, the baseline, was in no skill.
- **Example profiles** for a made-up Go service (reliab's own shape), Rust
  workspace, and .NET billing service (.NET Framework 4.7.2 and .NET 8 with
  NUnit TRX, core FsCheck, Verify.NUnit, Stryker.NET, Schemathesis, AltCover,
  and a `dotnet test --filter` impacted map) live in `templates/examples/`.
  A test loads each against the profile schema and checks that every filled
  role has a verified date and no command is wired for a role marked not
  applicable. A plugin cannot read files outside its directory, so the skill
  ships copies in `plugin/skills/onboard/examples/`, and the test keeps them
  identical. Tool names stay in examples and adapters, never in the core.

## Departures from the paper

- The paper's S0 converts test projects to the ecosystem's modern build
  format. The skill does not: that edits the project, so it is an open
  question in the S0 report.
- The paper runs the probes on a CI agent too. reliab writes no CI
  configuration ([ci-agnostic](/decisions/ci-agnostic.md)), so the CI probe
  is the person's step after the report, and `verified_on` stays unset
  until it is done.
- The paper counts quarantined flaky tests as a health metric. Here a flaky
  test is a classified finding a person decides about; the agent never
  quarantines one.

## Consequences

- The quality of S0 now depends on the skill's instructions; it is accepted
  by real use (a fresh agent onboarding a repository that never saw reliab),
  like every roadmap step.
- The examples use output globs, `changed_only`, `{base}`, `base`,
  `impacted.separator`, `protected_edits`, and `reliab-adapter-verify`, so
  the example test fails if the core drops any of them.
- Tool caveats (a "since" mode that assumes `master`, result files a runner
  overwrites, seeds a property library ignores, encodings on Windows, a
  second runtime's build host) are written in the skill as claims to probe
  and record, never as reliab features.
- `schemas/testdata/example-rust.profile.yaml` predated the adapters and
  was superseded by `templates/examples/rust.profile.yaml`; it was removed
  on 2026-10-18, and the profile tests load the examples instead.

## Acceptance by real use (2026-10-18)

A made-up .NET service (net8.0 and net472 projects, NUnit, FsCheck, Verify,
Stryker.NET, AltCover, a hand-written OpenAPI spec) with no trace of
reliab was onboarded by a fresh headless session that had only the plugin.
It asked six questions, then finished S0 in about 25 minutes. Every role
was probed through reliab, including planted failures in a scratch
worktree. It recorded four corrections with quoted errors, and the
timings it reported match the reports (inner 10 s, prepr 167 s). It made
no product or test edits and classified the two existing API defects for
a person. A second fresh session then took a ticket to ready-for-PR:
properties citing the ticket and the spec, inner and prepr, a reviewer
fail and then a pass, and the summary pasted unchanged.

The skill gained what the rehearsal showed it lacked: the shipped adapters
by name (snapshots were read without their diff label), a check that a
tier's commands see the current build (a mutation tool restored an old
binary under a later coverage command), the linked-worktree caveat, and
keeping writes and reads of `.reliab/` in separate shell commands.
Gaps that need core changes are reported, not made here: the pre-bash
heuristic treats every path in a writing command as a target; a finding
classified and then fixed by the next run loses its classification in the
summary; a fuzz finding whose requirement the spec does not state has no
kind but `legacy-defect-or-quirk`, whose canned question says
"pre-existing behavior"; and a role that is needed but missing can only be
written `applicable: false`.
