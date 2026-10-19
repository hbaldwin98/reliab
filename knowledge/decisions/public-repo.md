---
type: Decision
title: "Public repository, no open-source license"
description: "reliab is public at hbaldwin98/reliab with no license file; the Go module path follows."
status: stable
tags: [repo]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Public repository, no open-source license

## Decision

- Repository: `github.com/hbaldwin98/reliab`, public from v0.1.0
  (2026-10-19; it was private until then). Release assets download over
  plain HTTPS with no credentials
  ([binary-distribution](/decisions/binary-distribution.md)).
- No open-source license file (all rights reserved), by the owner's choice
  for now.
- Go module path: `github.com/hbaldwin98/reliab`.
- Default branch: `main`.
