---
type: Decision
title: "Explicit path classes; unmatched files count as product"
description: "The profile declares product, tests, and ignore globs; anything unmatched is product, so gaps make hooks stricter."
status: stable
tags: [profile, hooks]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Explicit path classes; unmatched files count as product

## Decision

The profile declares `paths: { product: [...], tests: [...], ignore: [...] }`.
A file matching none counts as **product** (fail-safe): a missing glob makes
the hooks stricter, never looser. `protected_paths` is a separate list.
