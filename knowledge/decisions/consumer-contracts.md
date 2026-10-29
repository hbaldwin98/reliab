---
type: Decision
title: "Consumer-driven contracts: a contracts command id and a pact adapter"
description: "A built-in contracts command id runs a consumer's tests against mocks of the services it calls (optional role T16) and writes the contracts they record to a pending directory under .reliab/out. reliab-adapter-pact compares each with its committed, protected contract and labels the difference; a person approves a changed contract, which is what the provider's team is given. Provider verification and publishing to a broker stay outside reliab."
status: draft
tags: [verify, adapters, contracts, departure]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-29T12:00:00-07:00
---

# Consumer-driven contracts: a contracts command id and a pact adapter

## Context

A service calls other services its organisation owns. Its tests can run
against a mock of each one that records what the service sent and what it
needs back, and the recording (a pact) is the contract the provider's team
then implements and verifies against (owner, 2026-10-29). The paper's roles
stub external dependencies for faults (T9) and fuzz an API from its own spec
(T7); neither records what a consumer depends on.

The owner scoped it to the consumer side: reliab generates and checks the
contracts of the repository it runs in. Publishing them to a Pact Broker is
deployment work for CI, and verifying them against a provider happens in the
provider's repository.

## Decision

- **`contracts`** is a built-in command id in inner, prepr, and nightly,
  after `spec_checks`. Consumer contract tests run against an in-process mock
  server, so they cost about what unit tests do. It depends on T16, a role
  reliab adds and a profile may leave out
  ([T16](/roles/t16-consumer-contracts.md)); marked not applicable,
  contracts is skipped as such. Use `relevant_when` to skip it when a change
  cannot reach a client of an owned service.
- **Committed contracts are oracles.** The accepted contracts live in a
  directory the profile lists in `protected_paths`
  ([R2](/rules/r2-protected-oracles.md)). The command never writes there: it
  points the tool's output directory at one under `.reliab/out` (for
  pact-js, pact-go, and PactNet, the `dir` or `PactDir` option; an
  environment variable the test setup reads), as insta's pending directory
  is in [snapshot-diff-labels](/decisions/snapshot-diff-labels.md). No new
  profile field or placeholder.
- **`reliab-adapter-pact`** (`cmd/adapters/pact`) reads the pending and
  accepted directories, both given as arguments, and writes findings
  ([external-adapters](/decisions/external-adapters.md)). It pairs files by
  consumer and provider name, not file name. Before comparing it renders
  each pact canonically: `metadata` dropped (library and specification
  versions change with no change to the contract), interactions sorted by
  description and provider state, JSON indented one field per line. It
  labels each difference with `pkg/snapdiff` and writes one finding per
  changed contract, named by consumer and provider and located at the
  accepted file. A pending contract with no accepted one is additive-only.
  An accepted one no test wrote is a values-changed finding, since the
  consumer stopped needing that provider or the tests stopped covering it,
  but only after a clean, whole run: a failing test may have kept Pact from
  writing it, and a run narrowed by `--scope` (`RELIAB_SCOPE`) writes only
  some. A clean run that wrote no contract while accepted ones exist is an
  error, since the pact directory points elsewhere. With `-junit` it keeps
  failing tests, as reliab-adapter-insta does. The command clears the
  pending directory first, because Pact merges into a file already there,
  and runs every contract test, never `{impacted}`.
- **The labels read the other way round from snapshots.** For a snapshot,
  additive-only is the quick approval. For a contract, an added interaction
  or field is a new demand on the provider, which its team must build;
  a removed one asks less of it. `report route` says so for a finding from
  a contracts command, and gives the same next step either way: a change
  that meant to change what the service needs is `characterization-diff`,
  and the agent stops for a person; one that did not is `new-code-defect`,
  and the agent fixes product code, never the contract.
- **A person approves a changed contract.** The agent copies the pending
  file over the accepted one, commits it with the change, and lists it as a
  question; the person runs `reliab approve`
  ([protected-path-approval](/decisions/protected-path-approval.md)). The
  approved, committed contract is what goes to the provider's team. `report
  summary` already lists approved protected-path changes, so the PR shows
  which contracts changed.
- **Out of reliab:** publishing contracts to a broker, verifying them
  against a provider, and deciding whether a deployment is safe
  ([ci-agnostic](/decisions/ci-agnostic.md)). A provider repository that
  uses reliab can run its pact verification as `integration_tests` today.

## Consequences

- Profiles without `contracts` are unchanged: it is skipped as having no
  command, and `roles` without T16 stays valid.
- A departure from the paper
  ([paper-as-design-intent](/decisions/paper-as-design-intent.md)): T16 and
  the `contracts` id are reliab's.
- The profile schema's command id enum, the tier defaults, the role map in
  `internal/verify/tiers.go`, the onboard skill (a step that asks which
  owned services the repository calls), the example profiles, and
  docs/adapters.md change with it.
- Pact is the first tool. Another contract tool needs only its own adapter;
  the command id, role, and approval flow do not name Pact.
