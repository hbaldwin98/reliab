---
type: Decision
title: "Shape of the 0.x profile and report schemas"
description: "Field-level choices in schemas/: fixed command ids, placeholders, impacted args, output formats, and report fields."
status: stable
tags: [schema, profile, report]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T16:05:00-07:00
---

# Shape of the 0.x profile and report schemas

## Context

[schemas-and-versioning](/decisions/schemas-and-versioning.md) settled the
format and strictness. Writing the schemas forced field-level choices.

## Decision

- **Versioning.** Both schemas start at `0.1.0`. Major 0 means breaking
  changes are still allowed; the CLI accepts only major 0.
- **Command ids are a fixed set**, taken from the paper's profile skeleton:
  `build`, `analyzers`, `corpus`, `unit`, `snapshots`, `properties`,
  `spec_checks`, `model_tests`, `api_fuzz`, `mutation_changed`,
  `mutation_full`, `faults_db`, `faults_http`, `store_tests`,
  `replay_sampled`, `replay_full`, `coverage`, `characterize` (added
  2026-10-18, run only by `reliab characterize`:
  [on-demand-characterization](/decisions/on-demand-characterization.md)),
  and `integration_tests` (added 2026-10-19: tests that start the
  application or need a real store or service, run in prepr and nightly;
  a consumer repository had filed them under `store_tests`, which means
  logic that lives in the store). `integration_tests` depends on no single
  role, so a role marked not applicable or missing never skips it; leave it
  out of the profile when the repository has no such suite. Tiers, reports, and findings
  all refer to these ids, so a typo is a validation error, not a silently
  skipped layer. `tiers.<tier>.run` (a list of ids) overrides the built-in
  tier mix.
- **A command is a string or an object.** A string runs and is judged by exit
  code. An object adds `format` (`junit`, `trx`, `sarif`, `mutation-report`,
  `cobertura`, `findings`, `none`), which picks the reader (`findings` is
  written by an external adapter: [external-adapters](/decisions/external-adapters.md)), and `output`, the file the
  adapter reads; `output` is required whenever `format` is not `none`.
- **Placeholders** in command strings: `{impacted}`, `{cases}`, `{seed}`,
  `{scope}` ([on-demand-verify](/decisions/on-demand-verify.md)), and
  `{capture}` (the `characterize` command only).
- **Impacted tests** are `impacted: { all: <args>, map: [{glob, args}] }`.
  Changed files matching a glob contribute its args; any unmatched changed
  file expands `{impacted}` to `all`. This refines
  [impacted-tests-map](/decisions/impacted-tests-map.md).
- **All fourteen roles are required**, each a filling (`tool`, with
  optional `version`, `verified`, `notes`), `applicable: false` with a
  `reason`, or (added 2026-10-18) `missing: true` with a `reason`: the
  codebase needs the role and has no tool for it yet. Onboarding a .NET
  repository had to write such roles as not applicable with the reason
  "missing", which reads as "not needed". A missing role's commands are
  skipped with the reason `role-missing` (report 0.8.0), `--only` refuses
  them, and `reliab doctor` warns about each. The shape is additive, so the
  profile's schemaVersion stays 0.1.0; an older reliab rejects a profile
  that uses it.
- **YAML is read strictly.** Duplicate keys, merge keys, and non-scalar keys
  are errors; dates such as `2026-10-17` stay strings.
- **Report.** `run` records tier, profile name, start time, elapsed and budget
  seconds, `complete`, `treeHash`, seed, property cases, each command's exit
  code, and `skipped` with reason `budget`, `no-command`, or
  `not-applicable`. Findings carry `id`, `kind`, `command`, `status` (`open`,
  `fixed`, `discarded`, `acknowledged`) and optional detail fields; a
  `classification` records the agent's reason and an `acknowledged` block
  records a human. A `verdict` is the reviewer's, bound to the tree it re-ran
  ([reviewer-verdict](/decisions/reviewer-verdict.md)). Report 0.4.0 and
  findings 0.3.0 add `run.head` and mutant counts (`mutants`: killed,
  survived, notCovered) ([pr-evidence-summary](/decisions/pr-evidence-summary.md)).
  Report 0.5.0 adds `run.base`, `changedSince` on mutant counts measured on
  changed lines only, a command's `coverage` (lines valid and covered, from
  Cobertura), and the skip reason `build-failed`
  ([external-adapters](/decisions/external-adapters.md),
  [verify-execution](/decisions/verify-execution.md)). The profile adds
  `base`, `protected_edits`, `impacted.separator`, a command's
  `changed_only`, and `output` globs; all are optional, so older profiles
  still validate.
  Report 0.6.0 and findings 0.4.0 add per-file mutant counts
  (`mutantFiles`), a mutation command's `score`, and the finding kind
  `below-threshold`; the profile adds a mutation command's `threshold` and
  `mutation_baseline`, and `schemas/baseline.schema.json` (0.1.0) is new
  ([mutation-threshold](/decisions/mutation-threshold.md)).
  Report 0.7.0 adds the skip reason `not-relevant` and a command's
  `relevance`; the profile adds a command's `relevant_when`
  ([relevance-skips](/decisions/relevance-skips.md)).
  Report 0.9.0 adds a finding's `fingerprint` and `reopened`: a
  command-level finding carries its classification only while its
  fingerprint is the same
  ([finding-classification](/decisions/finding-classification.md)).
  Report 0.10.0 adds `from` to a finding's `classification` and
  `acknowledged`: set only on a carried decision, it names the report the
  decision was made in, as its tier and run start
  ([finding-classification](/decisions/finding-classification.md)).
  Report 0.11.0 adds `run.tests`, `run.mutationTests`, and the skip reason
  `needs-input`; the profile adds a command's `needs`
  ([named-tests](/decisions/named-tests.md)).
  An optional `diff` (`additive-only` or `values-changed`) labels a snapshot or capture
  that no longer matches, in the report (0.3.0) and the findings format
  (0.2.0) alike ([snapshot-diff-labels](/decisions/snapshot-diff-labels.md)).

## Consequences

reliab's own profile is `reliability-profile.yaml` at the repo root; the
example profiles for made-up Go, Rust, and .NET codebases are
`templates/examples/*.profile.yaml`. All are validated by tests.
