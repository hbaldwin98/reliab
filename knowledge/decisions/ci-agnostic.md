---
type: Decision
title: "CI-agnostic: no CI templates, gates are commands"
description: "reliab writes no CI files or CODEOWNERS; CI runs documented commands and reads exit codes, and wires approvals with its own host features."
status: stable
tags: [ci, departure]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# CI-agnostic: no CI templates, gates are commands

## Context

The paper has `init` scaffold CI gates and CODEOWNERS. Both are host-specific.

## Decision

- `reliab profile init` creates no CI configuration of any kind and no
  CODEOWNERS file.
- The agent-free gates are commands with exit codes:
  `reliab verify --tier prepr` on every push, `reliab verify --tier nightly` on
  a schedule, and `reliab gate protected --base <ref>`, which exits non-zero
  listing protected files changed since `<ref>`.
- Requiring human approval for protected changes is the host's feature (code
  owners, required reviewers). The docs explain the wiring in generic terms;
  `init` prints the protected paths so the user can configure their host.
- CI installs the pinned binary with the host-neutral install scripts
  ([binary-distribution](/decisions/binary-distribution.md)).

## Consequences

The paper's "everything CI needs lives in the repository" still holds for the
profile, deny rules, and pin; the CI pipeline file itself is the user's.
reliab's own repository has its own pipeline
([own-release-pipeline](/decisions/own-release-pipeline.md)); that is not a
template.
