---
type: Decision
title: "One repo: CLI, schemas, plugin, and a plugin marketplace"
description: "The repo root is a Claude Code marketplace; the plugin lives in plugin/ beside the CLI and schemas."
status: stable
tags: [repo, plugin]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# One repo: CLI, schemas, plugin, and a plugin marketplace

## Decision

```
.claude-plugin/marketplace.json   marketplace: one plugin "reliab", source "./plugin"
cmd/reliab, internal/             the Go CLI
schemas/                          profile and report JSON Schemas
plugin/                           .claude-plugin/plugin.json, hooks/, skills/, bin/
templates/                        what `reliab profile init` writes
testdata/                         fixture service for reliab's own tests
knowledge/                        this OKF bundle
```

Install: `/plugin marketplace add hbaldwin98/reliab`, then
`/plugin install reliab@reliab`. The repository is public, so this needs no
git credentials.

## Consequences

Per-stack profile plugins (paper §7.6) become more entries in the same
marketplace when needed, not in v0. A repo using reliab can register the
marketplace and enable the plugin in its own `.claude/settings.json`
(`extraKnownMarketplaces`, `enabledPlugins`), which `reliab profile init`
writes; collaborators get it after accepting the workspace trust dialog.
Because the plugin is a relative-path source, no separate install record is
needed.
