---
type: Role
title: "T3: Snapshot / approval testing"
description: "Serialize a response, diff against a committed file, pluggable scrubbers."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§4, Table 3"
---

# T3: Snapshot / approval testing

**Must:** serialize a response, diff it against a committed file, and support pluggable scrubbers.

**Example fillings (illustrative, unverified):** insta, syrupy, Jest snapshots, Verify, ApprovalTests.

A profile fills this role or marks it `applicable: false` with a reason, and
verifies the filling on the real codebase in stage S0.
