---
type: Decision
title: "Fuzzing and end-to-end tests are command ids with tier time boxes"
description: "Two built-in command ids join prepr and nightly: fuzz (coverage-guided fuzzing, optional role T15), time-boxed by the tier's fuzz_seconds (120 at prepr, 1800 at night), and e2e (the whole system), which depends on no role. Speed comes from configuring each tool per project, which onboarding records."
status: stable
tags: [verify, tiers, fuzzing, departure]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-21T12:00:00-07:00
---

# Fuzzing and end-to-end tests are command ids with tier time boxes

## Context

reliab is meant to be one tool for running a project's tests, by a person
at the CLI or by an agent: the usual suite, and on demand mutation,
property, fuzz, and end-to-end runs (owner, 2026-10-21). The paper's roles
cover API fuzzing from a spec (T7) but not coverage-guided fuzzing of code
below the API, and its tiers name integration tests but not tests of the
whole system. A fuzzer also differs from the other commands: it has no
natural end, so it needs a time it may explore.

## Decision

- **`fuzz`** is a built-in command id in prepr and nightly. It depends on
  T15, a role reliab adds and a profile may leave out
  ([T15](/roles/t15-fuzzing.md)); marked not applicable, fuzz is skipped as
  such.
- **`fuzz_seconds`** is a tier field, like `property_cases`: 0 at inner, 120
  at prepr, 1800 at night, each overridable in `tiers`. Commands get it as
  `{fuzz_seconds}` and `RELIAB_FUZZ_SECONDS`. 0 means replay the saved
  corpus only. The command splits the time among its targets; a helper
  script does it where the tool runs one target at a time (`go test -fuzz`).
- **`e2e`** is a built-in command id in prepr and nightly, after
  `integration_tests`, and depends on no role. Use `relevant_when` to skip
  it when a change cannot reach it.
- Saved crashing inputs are regression seeds: listed in `protected_paths`,
  committed with their fix, and replayed by `unit` or `corpus` in every
  tier.
- **Speed is per tool.** How fast a mutation, fuzz, property, or end-to-end
  run is depends mostly on how the tool is invoked (diff-only modes, job
  counts, time boxes, sharding, a reused build), and that is specific to the
  tool and the project. The onboard skill asks for it per tool and records
  the timings; reliab's own runner is not where that time goes.

## Consequences

- Profiles without `fuzz` or `e2e` are unchanged: both are skipped as
  having no command, and `roles` without T15 stays valid.
- A departure from the paper ([paper-as-design-intent](/decisions/paper-as-design-intent.md)):
  T15 and the `fuzz` and `e2e` ids are reliab's.
- The `fuzz` skill runs fuzzing on demand for an area, as the `mutation`
  skill does for mutation.
