---
name: evidence-stack
description: Background on reliab's method - evidence layers, finding kinds, the nine hard rules, and starting conditions. Consult when writing or judging tests in a reliab repository.
user-invocable: false
---

# The evidence stack

No single technique proves code correct. Each layer answers one question with
its own oracle, so layers fail independently. Agreement is strong evidence;
disagreement is itself a finding.

| Layer | Question | Oracle |
|---|---|---|
| 0 Static analysis | Is it obviously wrong? | Compiler, analyzers |
| 1 Characterization, regression corpus | Did existing behavior change? Do past failures stay fixed? | Recorded behavior, saved inputs |
| 2 Specification properties, metamorphic relations, model-based tests, API fuzzing | Does new code do what was intended? | Invariants from stated intent |
| 3 Mutation testing | Would the tests catch a bug here? | Mutants killed |
| 4 Fault injection, store-level tests | Does it fail safely? | No partial writes, bounded retries, no hangs |
| 5 Differential replay | Does new code match old on real inputs? | The previous build |

Characterization tests pin actual behavior on purpose, bugs included.
Specification tests state intended behavior. When they conflict, a human
decides which is right.

## Hard rules

- R1 A failing test you wrote is a finding. Fix product code; never change an
  assertion, widen a tolerance, or add a catch block to make it pass.
- R2 Snapshots, the regression corpus, scrubbers, mutant suppressions, and
  hand-maintained API specs change only with human approval.
- R3 Every specification property cites its source of intent in a comment: a
  ticket, a document, a reviewed rule card, or a caller that depends on it.
  Read the intent before the implementation.
- R4 Reproduce every failure from its seed, shrink it, and classify it before
  reporting.
- R5 Review your own tests: does each reach the changed code, could it pass
  vacuously, is it grounded?
- R6 Rank findings; only the most serious interrupt a human.
- R7 Surviving mutants on changed lines, not coverage gaps, drive new tests.
  Kill one only when its behavior is intended, citing where; classify an
  equivalent mutant `equivalent-mutant` and one whose behavior nothing states
  `requirement-silent`, each with a one-line reason.
- R8 Every run is time-boxed and reports what it skipped and why.
- R9 Behavior that looks wrong but a snapshot pins is escalated, never fixed.

## Finding kinds

`unclassified` (classify it), `new-code-defect`, `harness-defect`,
`wrong-property` (you resolve these), `legacy-defect-or-quirk`,
`characterization-diff`, `equivalent-mutant`, `requirement-silent`,
`environment` (a human decides), and `below-threshold` (a mutation score
short of its threshold or baseline: handle its survivors, then a human
decides; never classified).

## Starting conditions

- greenfield: the oracle is stated intent; properties and mutation lead.
- maintained: intent plus a suite of unknown adequacy; a mutation run over the
  existing suite is the first finding.
- legacy: yesterday's behavior is the oracle; characterization leads, and
  legacy-versus-intended questions go to a product owner.
