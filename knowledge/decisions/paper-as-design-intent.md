---
type: Decision
title: "The paper is design intent, not a fixed spec"
description: "reliab keeps the structure of the evidence-stack paper and records every departure as a Decision."
status: stable
tags: [process]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# The paper is design intent, not a fixed spec

## Context

The [paper](/references/evidence-stack-paper.md) specifies the plugin and CLI
but says itself that they are "a design, not yet a measurement" (§12).

## Decision

Keep the paper's structure: layers, roles, rules, tiers, report, plugin
anatomy. Where reliab departs from it, record the departure as a Decision in
this bundle and say why.

## Consequences

Departures so far: [finding-classification](/decisions/finding-classification.md)
(adds `unclassified`), [ci-agnostic](/decisions/ci-agnostic.md) (no CI
templates, no CODEOWNERS), [pre-push-v0](/decisions/pre-push-v0.md) (verdict
check off), [report-storage](/decisions/report-storage.md) (location),
[enforcement-hooks-self-contained](/decisions/enforcement-hooks-self-contained.md)
(no `if` filters on enforcement hooks),
[inner-tier-trigger](/decisions/inner-tier-trigger.md) (inner tier when a change
is done, not after each edit).
