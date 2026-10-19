---
type: Decision
title: "First targets: reliab itself and a real consumer repository"
description: "v0 is proven on reliab's own repo (Go, greenfield) and on a real consumer repository in another ecosystem, plus a fixture service under testdata/."
status: stable
tags: [targets, profile]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# First targets: reliab itself and a real consumer repository

## Context

The paper's main limitation (§12) is one instantiation on one starting
condition; its test of generality is a second profile on a different ecosystem
and starting condition, asking which roles survive unchanged.

## Decision

- **reliab itself** (Go, greenfield): go test via gotestsum (JUnit output),
  gremlins for mutation (go-mutesting if gremlins fails S0), rapid or
  `testing/quick` for properties, golangci-lint for static analysis.
- **A real consumer repository** in a different ecosystem and starting
  condition ([maintained](/concepts/starting-condition.md)). Its profile and
  records live in that repository, not in this bundle; reliab stays
  consumer-neutral ([context-agnostic](/decisions/context-agnostic.md)).
- A small fixture service under `testdata/` serves reliab's own CLI tests. Its
  language is open: [fixture-language](/open-questions/fixture-language.md).

## Consequences

Two ecosystems and two starting conditions from day one. Every role marked not
applicable is evidence for the paper's open question 1 (which roles are
universal).
