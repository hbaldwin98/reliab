---
type: Decision
title: "Roadmap to the agentic change flow"
description: "What reliab and a consumer's profile need for an agent to take a change edit to PR with the evidence the paper's change timeline shows, plus on-demand testing; CI and nightly work are deferred to the end."
status: draft
tags: [roadmap, plugin]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T09:30:00-07:00
---

# Roadmap to the agentic change flow

## Context

The paper's change timeline (§10.3, Table 13, Figure 11) shows one change
taken edit to PR by an agent: impacted tests, properties from intent, a
failure reproduced and fixed in product code, a characterization diff that
catches a side effect the new tests missed, mutation on changed code, an
endpoint fuzz, a justified skip, a self-review, and a PR summary a human can
trust. It assumes adoption stages S0 to S5 are complete
([evidence-stack](/concepts/evidence-stack.md)).

On 2026-10-18 reliab has the tiers, report, classification and routing,
enforcement hooks, and mutation findings through adapters
([external-adapters](/decisions/external-adapters.md)). Mapped against the
timeline:

| Moment | Needs | reliab side |
|---|---|---|
| Classify the diff, run impacted tests and the corpus | path classes, impacted map, corpus | mostly; a consumer's saved regression cases need a corpus command |
| Properties from intent, citing their source | properties skill, R3 | instructed, not checked |
| Failure reproduced, classified, fixed in product code | R1, R4, classify, route, stop hook | done |
| Characterization diff catches a side effect | S2 snapshots over an in-process host (T5) | built (diff labels, `characterize`) |
| Mutation on changed code; survivors killed or explained | T4, R7 | done; threshold and per-scope baseline built ([mutation-threshold](/decisions/mutation-threshold.md)) |
| Endpoint fuzz | S3 generators per request type, catch-all fuzz | consumer-side; read through the profile's `api_fuzz` command |
| A skip justified by the change ("writes nothing") | S4 faults; relevance skips | faults consumer-side (`faults_db`); relevance skips built ([relevance-skips](/decisions/relevance-skips.md)) |
| Self-review, ranked findings, PR summary | reviewer agent, R6, summary | summary and reviewer verdict built; R6 open |

## Decision

Build toward the flow in this order. **CI and nightly work are deferred to
the end** (owner's call, 2026-10-18): the agentic flow comes first.

1. **On-demand testing.** `reliab verify --only <command>... [--scope
   <paths>]` runs chosen commands against chosen paths, writing a normal
   report and findings. User-invocable skills steer one kind of testing at
   one area through the full loop: mutation (find survivors, kill or explain
   them), properties (write properties from stated intent), and later
   characterize and fuzz as those layers exist. Done when a person can point
   the agent at an area and a kind of testing and get findings handled per
   the triage table.
2. **A consumer's evidence layers, S2 first.** T5: the service in-process
   on a temp endpoint, driven through its own client; S2, S3, and S4 all
   stand on it. S2, sized for a maintained codebase (owner's call,
   2026-10-18): the paper's Table 1 has a maintained service "pin the
   most-used endpoints first" and makes characterization the largest piece
   only for legacy code and platform moves, so a maintained consumer pins
   **a lean set** of its most-used conversations as snapshots, protected
   like its other oracles, to catch side effects that leak into unrelated
   responses. Each response type appears in a few snapshots, not every one,
   so a deliberate field touches a handful of files; reliab labels each
   snapshot diff **additive only** (new fields, nothing else changed) or
   **values changed or removed**, so a deliberate addition is a quick
   approval and anything else gets real review (approval stays human, R2).
   Characterization's main use is **on demand around refactors and
   migrations**: a `characterize` skill (the paper's own) captures an area's
   behavior, the change is made, the capture is compared, then discarded;
   nothing is committed or approved. Ordinary feature work is carried by
   specification oracles (properties, S3), which a deliberate change does
   not churn. Then S3 (a generator per request type, a generator-coverage
   test, a catch-all "every request gets a reply, nothing panics" fuzz, and
   a changed-code mutation threshold against the baseline) and S4 (a
   fault-injecting store wrapper and no-partial-write tests for critical
   writes). Each stage is done at its Table 14 exit criterion.

   *Built in reliab (2026-10-18):* the changed-code mutation threshold
   and the baseline ratchet: a mutation command's `threshold`, a protected
   baseline file of scores per scope that a person writes with `reliab
   baseline write`, a derived `below-threshold` finding that routes to the
   survivors and is never classified, the score in `report summary`, and
   `reliab gate mutation` for CI
   ([mutation-threshold](/decisions/mutation-threshold.md)). Accepted on
   reliab's own repository; a consumer sets its own threshold and records
   its baseline.

   *S3 at scale:* on a large codebase the generators come from a schema
   fuzzer or type-derived generators, not one hand-written generator per
   type; hand-written generators go only where they pay off (a type with
   invariants the derivation cannot see). Generator coverage is a baseline
   ratchet, like the mutation score: recorded first, then held.

   *Built in reliab (2026-10-18):* snapshot diff labels, a generic rule in
   `internal/snapdiff` carried as an optional `diff` field on findings and
   shown by route, with `reliab-adapter-insta` reading insta's pending
   files ([snapshot-diff-labels](/decisions/snapshot-diff-labels.md)); and
   `reliab characterize capture | compare | discard` over a `characterize`
   profile command, with the `characterize` skill
   ([on-demand-characterization](/decisions/on-demand-characterization.md)).

   *Done (2026-10-18):* S2, S3 and S4 each reached their exit criterion on
   a real consumer repository, each with a real-use acceptance: a snapshot
   diff labelled and stopped on; a planted pre-existing crash that a fresh
   session, given an unrelated task, let the fuzz find, reproduced from its
   seed, classified as legacy, and left for a person; and every critical
   store write held whole or absent under injected faults, which found one
   real partial write. The inner tier's fuzz caught a one-request crash in
   about 70% of runs at 100 cases, so a consumer should size inner's fuzz
   cases by measurement against a planted crash, not by default.
3. **A legible change.** `reliab report summary` and a skill that writes the
   PR summary from the report (what ran, what was skipped and why, mutation
   results, findings fixed, questions for a human); a reviewer subagent that
   re-runs verify in a clean worktree without edit tools, checks intent
   citations, and writes a verdict the pre-push hook requires; an R6 ranking
   once its rubric is decided ([r6-scoring-rubric](/open-questions/r6-scoring-rubric.md)).

   *Built (2026-10-18):* `reliab report summary`, the `summary` skill, the
   `reliab:reviewer` agent, and `reliab report verdict`, bound to the
   reviewed tree ([pr-evidence-summary](/decisions/pr-evidence-summary.md),
   [reviewer-verdict](/decisions/reviewer-verdict.md)). Accepted on
   reliab's own repository; the replay on a consumer repository is still to
   do. R6 stays open.

   *Relevance skips built (2026-10-18):* a command's `relevant_when`
   globs; inner and prepr skip a command no changed file matches, with
   reason `not-relevant` and what the change touched, and record why the
   others ran; nightly, `--only`, and an unresolvable base never skip;
   summary and route show both ([relevance-skips](/decisions/relevance-skips.md)).
   Accepted on reliab's own repository. A consumer declares its own
   relevance rules, starting with the layers its changes most often cannot
   reach (fault injection, store tests, endpoint fuzz).
4. **Proof the rules hold.** The §7.5 eval suite: one fixture case per hard
   rule, fresh headless sessions with and without the plugin, several
   repeats, graded by trace and file assertions. Run when skills, hooks, the
   AGENTS template, or the pinned model change; never on every commit. The
   suite loads only the plugin under test, never the operator's user-level
   plugins.
5. **Deferred to the end:** releasing v0.1.0 so CI can pin reliab, the CI
   gates, the nightly tier and its commands, the morning triage workflow,
   and the committed findings queue ([acks-are-local](/open-questions/acks-are-local.md)).

**Every step ends with real use, not only green tests** (owner's call,
2026-10-18). Unit tests and a clean prepr show the code works; a step is
done only when its "done when" holds in use: the CLI behaves as specified on
a real repository, and a fresh agent session driven the way a person would
drive it (a skill invoked on an area, a replayed change) does the work the
step promises. Each step's acceptance:

- Step 1: `verify --only … --scope …` on reliab and on a real consumer
  repository; the stop hook and push gate ignore the on-demand report; a
  headless session invoking `/reliab:mutation <area>` kills or classifies
  the survivors.
- Step 2: each stage's Table 14 exit criterion on a real consumer
  repository, plus a session in which an agent's change trips the new layer
  (a snapshot diff it must stop on, a fuzz crash it must reproduce).
- Step 3: a replayed change on a real consumer repository produces a PR
  summary and a reviewer verdict a person can check against the report.
- Step 4: the eval suite itself is the acceptance.

## Consequences

- A consumer may run the plugin ahead of S2 to S4, which Table 14 places
  before S5. This is a recorded departure: until those layers exist, an
  agent change there is checked by unit tests, impacted properties, and
  mutation on changed lines only.
- A paired replay pilot (tasks run with and without the plugin) measures S5
  behavior and changed-code mutation score; it is a pilot, not the §7.5
  suite.
