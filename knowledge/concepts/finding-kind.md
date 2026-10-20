---
type: Concept
title: Finding kind
description: The triage classification every failure receives, which decides who resolves it.
tags: [method, report]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: §6, Figure 11; Appendix A Listing 5
---

# Finding kind

| Kind | Resolved by | Action |
|---|---|---|
| unclassified | agent | Reproduce, shrink, classify (reliab-specific; see below) |
| new-code-defect | agent | Fix product code, re-run |
| harness-defect | agent | Fix the test harness, re-run |
| wrong-property | agent | Delete the property, write down why |
| legacy-defect-or-quirk | human | Findings queue; a human decides |
| characterization-diff | human | Stop; a human approves or reverts |
| equivalent-mutant | human | Stop; a human confirms, then acknowledges or excludes it (reliab-specific; see below) |
| requirement-silent | human | Stop; a human says whether the mutated, generated-input, or under-fault behavior is intended (reliab-specific; see below) |
| environment | human | Stop; a human fixes the machine, then re-runs (reliab-specific; see below) |
| below-threshold | human, after its survivors | Handle its survivors; then stop, a human accepts the score or says what to test (reliab-specific; see below) |

`unclassified` is not in the paper. Adapters can see that a test failed but
not why, so every adapter-produced finding starts unclassified and the agent
classifies it with `reliab findings classify`. See
[finding-classification](/decisions/finding-classification.md).

`equivalent-mutant` is not in the paper either. The paper's R7 has the agent
log an equivalent mutant (one that cannot change behavior, so no test can
kill it) with a one-line reason; the kind is where that reason is recorded,
and only findings from `mutation_*` commands can take it.

`requirement-silent` is reliab's name for the paper's other mutation
outcome: "if the requirement is silent, ask." A surviving mutant whose
behavior nothing states is a question for a human, not a test to write. A property or fuzz failure on an input the cited source says nothing
about is the same question, and so is a fault test's failure where nothing
states what should happen under the injected fault, so those commands'
findings can take it too.

`environment` covers a failure the machine caused, not the code: resource
exhaustion, network, a service the tests need. The paper treats such noise
through its flaky-test quarantine metric; without a kind, agents filed it
as a legacy quirk, which means something else. It never carries to the next
run.

`below-threshold` is a mutation score short of the profile's threshold or a
baseline scope's score. It is derived from the survivors, never classified,
and adds no work of its own: the survivors carry it
([mutation-threshold](/decisions/mutation-threshold.md)).
