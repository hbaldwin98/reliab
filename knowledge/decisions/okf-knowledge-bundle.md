---
type: Decision
title: "Project knowledge is an OKF v0.2 bundle in knowledge/"
description: "Decisions and understanding are stored as Open Knowledge Format concepts, not a separate ADR folder."
status: stable
tags: [process, knowledge]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Project knowledge is an OKF v0.2 bundle in knowledge/

## Decision

- `knowledge/` is one Open Knowledge Format v0.2 bundle; the root `index.md`
  declares `okf_version: "0.2"`.
- Types: `Decision`, `Concept`, `Rule`, `Role`, `Layer`, `OpenQuestion`,
  `Source`.
- Decisions are `decisions/<slug>.md` (no numbers; the path is the identity),
  with `status` and ADR-style body sections (Context, Decision,
  Consequences). A superseded decision becomes `status: deprecated` and links
  to its replacement.
- Trust fields: `generated` records the drafting agent; `verified` is added
  when a human reviews the concept.
- `log.md` gets a dated entry whenever concepts change.
- A Go test checks conformance: parseable frontmatter, non-empty `type`,
  index/log structure; broken links warn, not fail.
- Section indexes (`<section>/index.md`) are generated, not written by
  hand: `go generate ./internal/okf` (or `go run ./internal/okf/okfindex`)
  writes one entry per concept, `* [title](file.md) - description` from its
  frontmatter, in file-name order, under the root index's link text for the
  section. A test fails when a checked-in index is out of date. Added
  2026-10-20: hand-kept entries had drifted from five decisions'
  descriptions and out of order, and two branches each appending an entry
  conflicted. The root index stays hand-written.
- `log.md` merges with git's `union` driver (`.gitattributes`), so two
  branches that each add a line under the same date merge without a
  conflict; check the merged order by eye.
