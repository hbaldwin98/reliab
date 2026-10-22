---
type: Concept
title: reliab
description: What reliab is, who it is for, and how its parts fit together.
tags: [overview]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
---

# reliab

reliab makes the agent contract from
[the evidence-stack paper](/references/evidence-stack-paper.md) executable and
enforceable. It gives a service a trustworthy oracle for AI-written code by
running independent layers of evidence
([the evidence stack](/concepts/evidence-stack.md)) inside fixed time budgets,
and by enforcing the [hard rules](/rules/) through the agent harness rather
than through prompts.

It has two parts:

- **The CLI** (`reliab`), a single Go binary. It reads a
  [stack profile](/concepts/stack-profile.md), runs the commands the profile
  names for each [tier](/concepts/tier.md), converts tool output into one
  report, and answers hook events with allow/deny decisions. CI runs the same
  binary with no agent present.
- **The Claude Code plugin**, shipped from this repository, which is also a
  plugin marketplace. Skills teach the agent when to run the CLI; hooks call
  the CLI; the repository's permission deny rules protect the oracles.

The plugin's on-demand skills test one area: `mutation`, `properties`,
`fuzz`, `characterize`, and `adapter`; `diagnose` (`/reliab:diagnose`)
takes a symptom to its cause and a failing test, running the test under the
language's debug adapter with `reliab debug run` when its output does not
show the cause ([debug-adapter-seam](/decisions/debug-adapter-seam.md)).

reliab is deliberately ignorant of stacks, CI systems, and where it is used.
Everything stack-specific lives in a profile; everything CI-specific is the
user's wiring of two commands and their exit codes. See
[context-agnostic](/decisions/context-agnostic.md) and
[ci-agnostic](/decisions/ci-agnostic.md).

The v0 scope is in [v0-scope](/decisions/v0-scope.md); the first real targets
are in [dogfood-targets](/decisions/dogfood-targets.md).
