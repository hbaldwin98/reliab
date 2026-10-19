---
type: Decision
title: "Relevance skips: a layer the change cannot affect is skipped, saying why"
description: "A profile command may declare relevant_when globs. In the inner and prepr tiers verify skips it, with reason not-relevant and a detail naming what the change touched, when no file changed since the resolved base matches; it never skips in nightly, under --only, or without a base, and records why the command ran. Summary and route show both."
status: draft
tags: [verify, tiers, report, profile]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T22:30:00-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§10.3 change timeline: a skip justified by the change (\"skips fault injection because the change writes nothing\")"
---

# Relevance skips: a layer the change cannot affect is skipped, saying why

## Context

The paper's loop classifies a diff (logic, endpoint, data store, I/O) and
skips the layers the change cannot affect, saying why. reliab skipped only
by tier, role applicability, budget, or a failed build, so every prepr run
paid for fault injection, store tests, or an endpoint fuzz on a change that
touched none of their code, and [R8](/rules/r8-time-box-and-report-skips.md)
had no way to say "not relevant" as a reason.

## Decision

- **The profile declares relevance per command:** `relevant_when`, a list
  of globs of the files that make the command relevant, e.g.
  `faults_db: { run: ..., relevant_when: ["src/Data/**", "**/*Repository*"] }`.
  A command without it is always relevant. The profile names files, so the
  core stays ignorant of what a "data store change" is in any stack.
- **Which files:** everything changed since the merge base with the
  resolved base, committed or not, untracked included (`gitx.ChangedSince`),
  with the base from the one resolution every changed-code question uses
  (`gitx.ResolveBase`: `--base`, the profile's `base`, then the fallbacks).
  The run records that base as `run.base`.
- **When it applies:** only in the inner and prepr tiers. A command whose
  globs match none of the changed files is skipped with reason
  `not-relevant` and a detail naming the base, up to three changed files,
  and the globs: "the change since main touches only README.md; it is
  relevant only to src/Data/**". With nothing changed, the detail says so.
- **When it never applies, and the command says why it ran** (its
  `relevance` in the report):
  - the nightly tier, which runs everything;
  - an on-demand run (`--only`), which runs what it was asked for;
  - a base that cannot be resolved or diffed: the command runs, and its
    relevance quotes the error. An unresolvable base never skips anything.
  A command that ran because a changed file matched records which file and
  glob: a product file when one matches, then a test file, then any other
  (added 2026-10-19: alphabetical order named a test seed ahead of the
  product file that made the command relevant).
- **R8: every skip is reported with its reason.** `reliab report summary`
  lists a relevance skip as "not relevant to the change" with its detail,
  and lists why each command with relevance rules ran; `reliab report
  route` prints both, telling the agent to report the skip in the PR
  summary.
- **The mutation gate** treats a mutation command skipped as not relevant
  as nothing to hold: there is no changed code it could score
  ([mutation-threshold](/decisions/mutation-threshold.md)).
- Report 0.7.0 adds the skip reason `not-relevant` and a command's
  `relevance`; the profile adds `relevant_when`. Both are optional.

## Consequences

- A relevance rule is a claim about the codebase, like a role filling: a
  glob that misses a file the layer depends on skips the layer wrongly.
  Since a narrower glob skips more, the rules are a person's to change:
  the profile lists itself in `protected_paths` (the owner's call of
  2026-10-19), so an agent's edit to `relevant_when` is denied, and a
  change to it is listed by `reliab gate protected` and the summary until
  a person approves it with `reliab approve`
  ([protected-path-approval](/decisions/protected-path-approval.md)). The
  nightly tier, which never skips, is the backstop, and the skip's detail
  names what changed so a reviewer can check the claim.
- On a branch with nothing changed since the base (main itself, say),
  every command with relevance rules is skipped in prepr; CI that wants
  those layers on main runs nightly.
- The skip and the reviewer's re-run agree: both judge the same tree
  against the same base.
