---
type: Decision
title: "reliab makes no assumptions about where it is used"
description: "The tool and repo never refer to any particular deployment, organisation, or private system."
status: stable
tags: [scope, privacy]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# reliab makes no assumptions about where it is used

## Context

reliab will be used in places that are not public. The tool should not care
where.

## Decision

- Nothing in the repository (code, docs, this bundle, schema examples, commits)
  names or describes any particular deployment, organisation, or private
  system.
- The paper's PDF is not committed; its [Source concept](/references/evidence-stack-paper.md)
  summarises the method only.
- All examples come from reliab's own profile or are neutral, made-up
  examples (such as a Rust workspace with a `service` crate). The bundle
  names no consumer repository; a consumer's profile and records live in
  that repository.
- Generic ecosystem support (for example parsing TRX, a common test-result
  format) is fine.

## Consequences

Stack-specific knowledge arrives only through profiles. CI integration is the
user's wiring ([ci-agnostic](/decisions/ci-agnostic.md)).
