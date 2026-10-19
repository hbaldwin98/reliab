---
type: Decision
title: "Report freshness is a working-tree hash"
description: "Reports record a tree hash built from a temporary git index; hooks compare it to the current tree."
status: stable
tags: [report, hooks]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Report freshness is a working-tree hash

## Context

The stop and pre-push hooks need to know whether the report is newer than the
last edit. Timestamps are fragile.

## Decision

A report records the tree hash of the working tree: tracked plus untracked
non-ignored files, added to a temporary git index (never the user's), then
`git write-tree`. The temporary index is seeded from the real one for speed
and keeps its mtime, so git still re-reads entries modified in the second
the index was written ([tree-hash-racy-edits](/open-questions/tree-hash-racy-edits.md)). Hooks recompute and compare. The profile can exclude paths
that should not invalidate a report (for example `docs/**`).
