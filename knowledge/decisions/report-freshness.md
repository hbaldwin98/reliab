---
type: Decision
title: "Report freshness is a working-tree hash"
description: "Reports record a tree hash built from a temporary git index; hooks compare it to the current tree. A tree that changed while the run ran makes the report stale for every tree."
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

A tree that changes while the run runs leaves a report that vouches for no
tree (added 2026-10-20): verify hashes the tree again when its commands
end, one more tree hash per run, and when the two differ records the end
hash as `run.treeHashEnd`. Every freshness check (`report.FreshFor`: the
stop and pre-push hooks, the summary, `gate mutation`, `baseline`, the
verdict) then calls the report stale, even once the edit is undone and the
tree hashes as it did at the start; the stop hook names the files that
changed during the run. Before, an edit made mid-run and then reverted left
a report that looked fresh though some commands had seen the edit.
