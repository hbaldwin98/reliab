---
type: Decision
title: "A person approves a protected-path change locally with reliab approve"
description: "reliab approve <path>... --by human:<name> records, in .reliab/approvals.json, the blob hashes of a protected file's change (on the merge base and in the working tree). gate protected, the PR summary, and the reviewer accept a change whose content matches an approval and list it apart; any later edit voids it. Hooks deny the command to every agent. Approvals are local, so CI and the code host's owner review stay authoritative."
status: draft
tags: [enforcement, hooks, reviewer, protected-paths]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T22:00:00-07:00
---

# A person approves a protected-path change locally with reliab approve

## Context

[R2](/rules/r2-protected-oracles.md) lets a protected oracle change only with
a person's approval, and the rules have an agent commit a property's saved
failing case (a regression seed) with its fix. Seed files sit under
protected paths, so `reliab gate protected` failed on every such change. In
step 3's real-use acceptance on a consumer repository the
[reviewer](/decisions/reviewer-verdict.md) rightly refused an approval the
author agent relayed ("the user approved it"), and reliab had no way for a
person to record one locally. So a change with a seed could never pass
review, and the PR summary said "Questions for a human: None" while it
waited on a person.

## Decision

- **`reliab approve <path>... --by human:<name> [--base <ref>]`** records,
  for each named file, an approval in `.reliab/approvals.json`: the path,
  the blob hash of its current content (empty for a deletion), its blob
  hash on the merge base (empty for a new file), the base ref and the merge
  base commit, who, and when. The base resolves as everywhere else (the
  flag, the profile's base, then `origin/HEAD`, `origin/main`, `main`,
  `master`). Each path must be a protected file the branch changed since
  the base; `--by` must be `human:<name>`. A new approval of a path
  replaces the old one.
- **Any protected file**, the mutation baseline included: the profile must
  list `mutation_baseline` under `protected_paths`
  ([mutation-threshold](/decisions/mutation-threshold.md)), so a person who
  ran `reliab baseline write` approves the result the same way.
- **The profile protects itself** (the owner's call of 2026-10-19). It
  holds what a change must pass: mutation `threshold`, `relevant_when`,
  `mutation_baseline`, and `protected_paths` itself. So
  `reliability-profile.yaml` is listed in `protected_paths` in reliab's own
  profile, in the template `reliab profile init` writes, and in the example
  profiles; a later edit is denied to agents and approved with `reliab
  approve reliability-profile.yaml`.
  - **Once committed.** The hooks and the deny rules `profile init` writes
    leave the profile alone while `HEAD` lacks it, so the agent onboarding
    a repository can write it; committing it turns its protection on, and
    re-running `reliab profile init` adds its deny rule (`reliab doctor`
    warns until then). The onboard skill tells the person so.
  - **The base's globs count too.** `reliab gate protected`, and so the
    summary and `reliab approve`, match changed files against the profile's
    `protected_paths` and those of the profile on the merge base, so a
    branch that drops a glob, or the profile's own entry, still has the
    change listed. A base profile that does not load is an error, not an
    empty list.
- **An approval covers that content only.** `reliab gate protected` matches
  each changed protected file against the approvals by path, merge-base
  blob, and working-tree blob. A match is listed under "approved by a
  person" with who and when, and does not fail the gate; anything else is
  listed as before and exits 1. Any later edit to the file, or a base
  whose version of it differs, voids the approval.
- **`reliab report summary`** lists every unapproved protected change since
  the resolved base under **Questions for a human**, numbered after the
  finding questions, with the command a person runs; approved changes
  follow with who approved them. With no base it says the protected paths
  were not checked. The JSON form adds `protected` (each change, with its
  approval) and `protectedError`.
- **Hooks deny it to every agent**, the way `findings ack` is denied: the
  pre-bash hook denies `reliab approve` (also `.exe`, `.cmd`, and a quoted
  path), and `.reliab/` is already denied to edits and shell writes. A
  person runs it in their own terminal, or in Claude Code with the `!`
  prefix, which does not go through the tool hooks.
- **The reviewer's R2 check** passes a protected change the gate lists as
  approved, still reads it as part of the diff, and fails one without an
  approval whatever the author says.
- **The rules say so**: AGENTS.md, the template, the verify, properties,
  and summary skills, and the session-start brief: commit the seed with its
  fix, list it as a question, and let a person run `reliab approve`.

## Limits

- **Approvals are local.** Like acknowledgements
  ([acks-are-local](/open-questions/acks-are-local.md)), they live in the
  gitignored `.reliab/` of one checkout, so on CI every protected change is
  unapproved and `reliab gate protected` fails there. That is intended: CI
  and the code host's owner review of protected paths
  ([ci-agnostic](/decisions/ci-agnostic.md)) stay authoritative. A local
  approval only lets the in-session flow (the reviewer, the push gate's
  report, the summary) reflect a decision a person already made.
- **It is not proof of a person.** The hook matches command text, as for
  every pre-bash rule, and `--by` is what the caller types. An agent that
  obfuscates the command could record one; that is a deliberate act, and
  the code host's review still decides. See the threat model in
  [reviewer-verdict](/decisions/reviewer-verdict.md).
- The approvals file is local state with its own `schemaVersion` (0.1.0),
  read only by reliab, and has no JSON Schema in `schemas/`.

## Consequences

- A change with a regression seed can pass review locally once a person
  approves the seed, and an edit after that makes it a question again.
- The gate's output and exit code change only when approvals exist, so CI
  sees the same gate as before.
