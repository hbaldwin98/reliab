---
type: Decision
title: "CLI and plugin share one version"
description: "One semver tag releases the binary and stamps the plugin version; consuming repos pin it in .reliab-version."
status: stable
tags: [release, plugin]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# CLI and plugin share one version

## Decision

- One semver tag `vX.Y.Z` releases the binary and sets `version` in the
  plugin's `plugin.json`, so the plugin's hooks always talk to a CLI they were
  written for. With `version` set, Claude Code updates users only when the
  string changes, not on every commit.
- A repo using reliab pins the same version in `.reliab-version`; CI installs
  exactly that.
- If the pin differs from the installed plugin, the session-start hook warns
  but does not block.
