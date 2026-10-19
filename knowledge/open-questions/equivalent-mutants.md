---
type: OpenQuestion
title: "How is an equivalent mutant closed"
description: "Closed: the agent classifies it equivalent-mutant, a human kind, and a person acknowledges it or excludes the mutant."
status: deprecated
tags: [findings, mutation]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T07:00:00-07:00
---

# How is an equivalent mutant closed

**Closed 2026-10-18.** A new kind, `equivalent-mutant`, routed as a human
kind ([finding-classification](/decisions/finding-classification.md)). The
agent records its one-line reason and stops; the stop hook does not block on
human kinds; a person acknowledges it or adds an exclusion to the mutation
tool's config (protected). The original question follows.

Surviving mutants are findings ([external-adapters](/decisions/external-adapters.md)).
The testing rules tell the agent to kill each one with a test, or say in one
line that it is equivalent: a mutation that cannot change behavior, which no
test can kill. None of the six kinds
([finding-classification](/decisions/finding-classification.md)) means that,
so an equivalent mutant stays unclassified. The stop hook blocks on
unclassified findings, and only a human can acknowledge one.

Options: a new agent kind (`equivalent-mutant`) that a human later confirms;
treating it as a human kind, so the agent stops and asks; or a committed,
protected suppression list such as cargo-mutants' `mutants.toml` exclusions,
a protected path in a typical Rust profile. Decide when a real session meets
a real case.
