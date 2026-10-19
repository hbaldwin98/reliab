---
type: Decision
title: "On-demand verify: chosen commands, chosen paths, a separate report"
description: "verify --only runs named profile commands under a tier's budget, --scope narrows them to paths, and the run writes an on-demand report that never satisfies the stop or push hooks."
status: draft
tags: [cli, verify, plugin]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T10:30:00-07:00
---

# On-demand verify: chosen commands, chosen paths, a separate report

## Context

Step 1 of the [roadmap](/decisions/agentic-flow-roadmap.md): a person points
the agent at an area and a kind of testing and gets findings handled per the
triage table. Tiers run a fixed layer mix over the change; this needs one
layer over an area the change may not touch.

## Decision

- **`--only <command>...`** runs just those profile commands, in the order
  given. A command the profile does not define is a config error, not a
  skip: it was asked for by name. `--tier` still sets the budget, property
  case count, and `RELIAB_TIER`.
- **`--scope <path>...`** (requires `--only`) names paths inside the
  repository, relative to where reliab runs. `{impacted}` is chosen from
  the files under the scope, in any tier, with the same map and fallback as
  for changed files; `{scope}` and `RELIAB_SCOPE` carry the paths,
  space-separated, for commands that narrow themselves. Without a scope both
  are empty.
- **The report's tier is `on-demand`** (`on-demand-latest.json`), recording
  `only` and `scope`. The stop hook and push gate read only the inner and
  pre-PR reports, so a narrow run can never stand in for them. `report
  route`, `findings classify`, and `findings ack` include it.
- **Fixed command ids still hold** ([schema-v0-shape](/decisions/schema-v0-shape.md)).
  "All mutants in an area" is `mutation_full` with a scope; "changed lines in
  an area" is `mutation_changed` with a scope.
- **Skills, not commands, carry the loop.** The plugin's `mutation` and
  `properties` skills are user-invocable and steer one kind of testing at one
  area: run, route, kill or write, classify, re-run, then the inner tier.
  The `characterize` skill followed on 2026-10-18, run through its own
  commands rather than `verify --only`
  ([on-demand-characterization](/decisions/on-demand-characterization.md));
  a fuzz skill follows when S3 exists.
- The report schema goes to 0.2.0: additive (the new tier value and two
  optional fields), so 0.1.0 reports still validate.

## Consequences

- A profile command that ignores `{scope}`, `{impacted}`, and
  `RELIAB_SCOPE` runs over everything even with a scope. reliab's own
  `scripts/mutation.sh` honors `RELIAB_SCOPE` per package, so a file in
  scope mutates its whole package.
- reliab's profile gains `mutation_full`, which the nightly tier also runs.
- An on-demand report carries classifications forward only from the previous
  on-demand report, whatever its commands or scope were.
