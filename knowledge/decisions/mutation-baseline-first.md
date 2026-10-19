---
type: Decision
title: "Mutation: baseline first, then hold the line"
description: "On a maintained codebase the first mutation run records a baseline with no threshold; the gate then requires changed code not to fall below it."
status: stable
tags: [mutation, gates]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§3.1, §11, §12 open question 2"
---

# Mutation: baseline first, then hold the line

## Decision

The first mutation runs have no break threshold. The first full run over the
existing suite records a baseline as a finding. After that, the gate requires
that mutation score on changed code does not fall below the baseline; the bar
can be raised later. This answers the paper's open question about the right
threshold by measuring first. See [layer 3](/layers/3-mutation.md).

Built as a profile threshold and a protected baseline file of scores per
scope, recorded by a person with `reliab baseline write`
([mutation-threshold](/decisions/mutation-threshold.md)).
