---
type: Decision
title: "Own JSON Schemas, semver, strict; SARIF and JUnit exports"
description: "Profile and report schemas are JSON Schema 2020-12 with a semver schemaVersion and strict keys; the report exports to SARIF and JUnit."
status: stable
tags: [schema, report]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Own JSON Schemas, semver, strict; SARIF and JUnit exports

## Context

The paper keeps a team-owned report schema on purpose (P7): no external
standard has `kind`, `intent`, or `replay`. Conventional standards still matter
for interoperability.

## Decision

- `schemas/` holds JSON Schema (draft 2020-12) for the profile (YAML) and the
  [report](/concepts/report.md) (JSON). CI validates example files against
  them.
- Each document carries `schemaVersion` (semver). Breaking changes bump major.
- The CLI rejects unknown keys and unknown major versions.
- `reliab report export --format sarif` (SARIF 2.1.0) and `--format junit`
  ship in v0, so any CI test tab or code-scanning view can consume results.
