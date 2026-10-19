---
type: Decision
title: "Impacted tests: a glob-to-arguments map in the profile"
description: "reliab stays stack-ignorant; the profile maps changed-file globs to test command arguments, falling back to everything."
status: stable
tags: [profile, cli]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Impacted tests: a glob-to-arguments map in the profile

## Decision

The impacted-tests map is profile data: a list of glob-to-command-arguments
entries. When no entry matches a changed file, verify runs everything. For a
Rust workspace, one entry per crate directory (`crates/service/**` maps to
`-p service`). Following reverse dependencies is deferred until shown
necessary.

When several entries match, their arguments are joined with
`impacted.separator`, a space by default. A test filter needs an operator
instead: `dotnet test --filter` takes one expression, so a .NET profile sets
`separator: "|"` and maps each project to a filter term such as
`FullyQualifiedName~Shop.Tests`. Added 2026-10-18; before it, such a profile
had to start every term with `|` after a dummy term.
